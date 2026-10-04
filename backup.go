package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

type backupTable struct {
	Name string  `json:"name"`
	Rows [][]any `json:"rows"`
}
type panelBackup struct {
	Format  string          `json:"format"`
	Version int             `json:"version"`
	Created string          `json:"created"`
	SHA256  string          `json:"sha256"`
	Data    json.RawMessage `json:"data"`
}

var backupColumns = map[string][]string{
	"settings": {"key", "value"}, "sources": {"id", "name", "url", "headers", "updated", "error"},
	"nodes": {"id", "source_id", "uri", "name", "host", "port", "transport", "security"},
	"rules": {"id", "priority", "name", "kind", "value", "target"},
}

func exportBackup() ([]byte, error) {
	tx, e := db.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	tables := []backupTable{}
	for _, name := range []string{"settings", "sources", "nodes", "rules"} {
		cols := backupColumns[name]
		rows, e := tx.Query("SELECT " + joinColumns(cols) + " FROM " + name + " ORDER BY 1")
		if e != nil {
			return nil, e
		}
		table := backupTable{Name: name, Rows: [][]any{}}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if e = rows.Scan(ptrs...); e != nil {
				rows.Close()
				return nil, e
			}
			table.Rows = append(table.Rows, values)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		tables = append(tables, table)
	}
	data, e := json.Marshal(tables)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(data)
	return json.Marshal(panelBackup{Format: "ngpanel-backup", Version: 1, Created: time.Now().UTC().Format(time.RFC3339), SHA256: hex.EncodeToString(sum[:]), Data: data})
}
func joinColumns(cols []string) string {
	s := ""
	for i, c := range cols {
		if i > 0 {
			s += ","
		}
		s += c
	}
	return s
}
func restoreBackup(raw []byte) error {
	var file panelBackup
	if e := json.Unmarshal(raw, &file); e != nil {
		return fmt.Errorf("Некорректный JSON бекапа")
	}
	if file.Format != "ngpanel-backup" || file.Version != 1 {
		return fmt.Errorf("Неподдерживаемый формат бекапа")
	}
	sum := sha256.Sum256(file.Data)
	if hex.EncodeToString(sum[:]) != file.SHA256 {
		return fmt.Errorf("Контрольная сумма бекапа не совпадает")
	}
	var tables []backupTable
	if e := json.Unmarshal(file.Data, &tables); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, t := range tables {
		cols, ok := backupColumns[t.Name]
		if !ok || seen[t.Name] || len(t.Rows) > 100000 {
			return fmt.Errorf("Некорректные таблицы бекапа")
		}
		seen[t.Name] = true
		for _, r := range t.Rows {
			if len(r) != len(cols) {
				return fmt.Errorf("Некорректная строка бекапа")
			}
			for _, v := range r {
				switch v.(type) {
				case string, float64:
				default:
					return fmt.Errorf("Некорректный тип поля")
				}
			}
		}
	}
	if len(seen) != 4 {
		return fmt.Errorf("Бекап неполный")
	}
	if !subscriptionLock.TryLock() {
		return fmt.Errorf("Дождитесь обновления подписок")
	}
	defer subscriptionLock.Unlock()
	if !probeLock.TryLock() {
		return fmt.Errorf("Дождитесь завершения проверки подключений")
	}
	defer probeLock.Unlock()
	deviceLock.Lock()
	defer deviceLock.Unlock()
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, name := range []string{"nodes", "sources", "rules", "settings"} {
		if _, e = tx.Exec("DELETE FROM " + name); e != nil {
			return e
		}
	}
	for _, name := range []string{"settings", "sources", "nodes", "rules"} {
		cols := backupColumns[name]
		marks := make([]string, len(cols))
		for i := range marks {
			marks[i] = "?"
		}
		for _, t := range tables {
			if t.Name != name {
				continue
			}
			for _, row := range t.Rows {
				if _, e = tx.Exec("INSERT INTO "+name+" ("+joinColumns(cols)+") VALUES ("+joinColumns(marks)+")", row...); e != nil {
					return fmt.Errorf("Бекап не восстановлен: %v", e)
				}
			}
		}
	}
	rowsRules, err := tx.Query("SELECT target FROM rules WHERE target LIKE 'node:%'")
	if err != nil {
		return err
	}
	var bound []string
	for rowsRules.Next() {
		var target string
		if err = rowsRules.Scan(&target); err != nil {
			rowsRules.Close()
			return err
		}
		bound = append(bound, target[5:])
	}
	err = rowsRules.Err()
	rowsRules.Close()
	if err != nil {
		return err
	}
	for _, id := range bound {
		var n int
		if err = tx.QueryRow("SELECT COUNT(*) FROM nodes WHERE id=?", id).Scan(&n); err != nil || n != 1 {
			return fmt.Errorf("В бекапе отсутствует подключение, указанное в правиле")
		}
	}

	var selected string
	tx.QueryRow("SELECT value FROM settings WHERE key='selected_node'").Scan(&selected)
	if selected != "" {
		var count int
		tx.QueryRow("SELECT COUNT(*) FROM nodes WHERE id=?", selected).Scan(&count)
		if count != 1 {
			return fmt.Errorf("В бекапе отсутствует выбранное подключение")
		}
	}
	var groupRaw string
	tx.QueryRow("SELECT value FROM settings WHERE key='balance_groups'").Scan(&groupRaw)
	var groups []BalanceGroup
	if groupRaw != "" {
		if json.Unmarshal([]byte(groupRaw), &groups) != nil {
			return fmt.Errorf("Некорректные группы в бекапе")
		}
	} else {
		var oldRaw string
		tx.QueryRow("SELECT value FROM settings WHERE key='balance_settings'").Scan(&oldRaw)
		if oldRaw != "" {
			var old BalanceSettings
			if json.Unmarshal([]byte(oldRaw), &old) != nil {
				return fmt.Errorf("Некорректная группа в бекапе")
			}
			if len(old.Nodes) > 0 {
				ids := append([]string{}, old.Nodes...)
				if selected != "" && !slices.Contains(ids, selected) {
					ids = append(ids, selected)
				}
				groups = append(groups, BalanceGroup{ID: "1", Name: "Группа автовыбора VPN", Nodes: ids, Interval: old.Interval})
			}
		}
	}
	if len(groups) > 16 {
		return fmt.Errorf("Слишком много групп в бекапе")
	}
	groupIDs := map[string]bool{}
	for _, g := range groups {
		normalizeGroup(&g)
		if e := validateGroupPolicy(g); e != nil {
			return e
		}
		if !validTarget("group:"+g.ID) || groupIDs[g.ID] || strings.TrimSpace(g.Name) == "" || len(g.Name) > 200 {
			return fmt.Errorf("Некорректная группа в бекапе")
		}
		groupIDs[g.ID] = true
		ids, e := balanceMembers(BalanceSettings{Nodes: g.Nodes, Interval: g.Interval}, "")
		if e != nil {
			return e
		}
		for _, id := range ids {
			var count int
			tx.QueryRow("SELECT COUNT(*) FROM nodes WHERE id=?", id).Scan(&count)
			if count != 1 {
				return fmt.Errorf("В бекапе отсутствует участник группы")
			}
		}
	}
	var mode string
	tx.QueryRow("SELECT value FROM settings WHERE key='default_route'").Scan(&mode)
	if mode == "" {
		mode = "direct"
	}
	if !validTarget(mode) {
		return fmt.Errorf("Некорректный маршрут по умолчанию")
	}
	targets := []string{mode}
	rowsTargets, e := tx.Query("SELECT target FROM rules")
	if e != nil {
		return e
	}
	for rowsTargets.Next() {
		var target string
		rowsTargets.Scan(&target)
		targets = append(targets, target)
	}
	rowsTargets.Close()
	for _, target := range targets {
		if !validTarget(target) {
			return fmt.Errorf("Некорректный выход в бекапе")
		}
		if strings.HasPrefix(target, "group:") && !groupIDs[strings.TrimPrefix(target, "group:")] {
			return fmt.Errorf("В бекапе отсутствует группа маршрута")
		}
		if strings.HasPrefix(target, "node:") {
			var count int
			tx.QueryRow("SELECT COUNT(*) FROM nodes WHERE id=?", target[5:]).Scan(&count)
			if count != 1 {
				return fmt.Errorf("В бекапе отсутствует подключение маршрута")
			}
		}
	}
	rawGroups, _ := json.Marshal(groups)
	if groupRaw != "" || len(groups) > 0 {
		if _, e = tx.Exec("INSERT INTO settings(key,value) VALUES('balance_groups',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(rawGroups)); e != nil {
			return e
		}
	}
	rows, e := tx.Query("PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	invalid := rows.Next()
	rows.Close()
	if invalid {
		return fmt.Errorf("Нарушены связи данных бекапа")
	}
	return tx.Commit()
}
func backupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		raw, e := exportBackup()
		if e != nil {
			http.Error(w, "Не удалось создать бекап", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=ngpanel-%s.json", time.Now().UTC().Format("20060102-150405")))
		w.Write(raw)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	if r.Header.Get("Origin") != "http://"+r.Host && r.Header.Get("Origin") != "https://"+r.Host {
		http.Error(w, "Invalid origin", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024*1024)
	e := r.ParseMultipartForm(16 * 1024 * 1024)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if e != nil {
		http.Error(w, "Бекап не прочитан (лимит 16 МБ)", 400)
		return
	}
	f, _, e := r.FormFile("backup")
	if e != nil {
		http.Error(w, "Выберите файл бекапа", 400)
		return
	}
	defer f.Close()
	raw, e := io.ReadAll(f)
	if e == nil {
		e = restoreBackup(raw)
	}
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	http.Redirect(w, r, "/?tab=backup&message="+"%D0%91%D0%B5%D0%BA%D0%B0%D0%BF+%D0%B2%D0%BE%D1%81%D1%81%D1%82%D0%B0%D0%BD%D0%BE%D0%B2%D0%BB%D0%B5%D0%BD.+%D0%94%D0%BB%D1%8F+%D0%B0%D0%BA%D1%82%D0%B8%D0%B2%D0%B0%D1%86%D0%B8%D0%B8+%D0%BD%D0%B0%D0%B6%D0%BC%D0%B8%D1%82%D0%B5+%D0%9F%D1%80%D0%B8%D0%BC%D0%B5%D0%BD%D0%B8%D1%82%D1%8C", 303)
}
