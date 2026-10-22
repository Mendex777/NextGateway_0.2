package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Keep saved targets and group membership. Reserve deleted IDs so a future import
// cannot silently attach those references to a different server.
func deleteConnections(id string, source bool) error {
	_, err := deleteConnectionsMatching(id, source, nil)
	return err
}

const missingNodeSuffix = " [исчез из подписки]"

func deleteMissingConnections(id, expected string) (int, error) {
	return deleteConnectionsMatching(id, true, &expected)
}

func deleteConnectionsMatching(id string, source bool, expectedMissing *string) (count int, err error) {
	if !subscriptionLock.TryLock() {
		return 0, fmt.Errorf("Обновление или удаление подключений уже выполняется")
	}
	defer subscriptionLock.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if expectedMissing != nil {
		var url string
		if err := tx.QueryRow("SELECT url FROM sources WHERE id=?", id).Scan(&url); err != nil {
			return 0, fmt.Errorf("Подписка отсутствует")
		}
		if url == "manual:" {
			return 0, fmt.Errorf("Очистка доступна только для подписок")
		}
	}
	query := "SELECT id FROM nodes WHERE id=?"
	if source {
		query = "SELECT id FROM nodes WHERE source_id=?"
	}
	if expectedMissing != nil {
		query += " AND COALESCE((SELECT value FROM settings WHERE key='node_missing:' || nodes.id), CASE WHEN substr(name,-" + strconv.Itoa(len([]rune(missingNodeSuffix))) + ")='" + missingNodeSuffix + "' THEN '1' ELSE '0' END)='1'"
	}
	rows, err := tx.Query(query, id)
	if err != nil {
		return 0, err
	}
	ids := []int{}
	for rows.Next() {
		var n int
		if err = rows.Scan(&n); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if !source && len(ids) == 0 {
		return 0, fmt.Errorf("Подключение уже удалено")
	}
	if expectedMissing != nil {
		actual := make([]string, len(ids))
		for i, n := range ids {
			actual[i] = strconv.Itoa(n)
		}
		slices.Sort(actual)
		expected := strings.Split(*expectedMissing, ",")
		slices.Sort(expected)
		if len(ids) == 0 || strings.Join(actual, ",") != strings.Join(expected, ",") {
			return 0, fmt.Errorf("Список исчезнувших подключений изменился. Обновите страницу и повторите удаление")
		}
	}

	for _, n := range ids {
		key := strconv.Itoa(n)
		if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES(?, '1') ON CONFLICT(key) DO UPDATE SET value='1'", "node_deleted:"+key); err != nil {
			return 0, err
		}
		if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES('node_id_floor',?) ON CONFLICT(key) DO UPDATE SET value=CAST(MAX(CAST(value AS INTEGER),CAST(excluded.value AS INTEGER)) AS TEXT)", key); err != nil {
			return 0, err
		}
		if _, err = tx.Exec("UPDATE settings SET value='' WHERE key='selected_node' AND value=?", key); err != nil {
			return 0, err
		}
		if _, err = tx.Exec("DELETE FROM nodes WHERE id=?", n); err != nil {
			return 0, err
		}
		if _, err = tx.Exec("DELETE FROM settings WHERE key IN (?,?,?)", "node_probe:"+key, "node_order:"+key, "node_missing:"+key); err != nil {
			return 0, err
		}
	}
	if expectedMissing != nil {
		removed := map[string]bool{}
		for _, id := range ids {
			removed[strconv.Itoa(id)] = true
		}
		if err = removeDeletedGroupMembers(tx, removed); err != nil {
			return 0, err
		}
	}
	if source && expectedMissing == nil {
		result, e := tx.Exec("DELETE FROM sources WHERE id=?", id)
		if e != nil {
			return 0, e
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return 0, fmt.Errorf("Подписка уже удалена")
		}
		for _, prefix := range []string{"source_disabled:", "sub_info:", "sub_interval:", "sub_attempt:"} {
			if _, err = tx.Exec("DELETE FROM settings WHERE key=?", prefix+id); err != nil {
				return 0, err
			}
		}
	}
	return len(ids), tx.Commit()
}

// Keep groups and their routing targets, pruning only confirmed deleted members.
func removeDeletedGroupMembers(tx *sql.Tx, removed map[string]bool) error {
	for _, key := range []string{"balance_groups", "balance_settings"} {
		var raw string
		if err := tx.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&raw); err == sql.ErrNoRows {
			continue
		} else if err != nil {
			return err
		}
		if raw == "" {
			continue
		}
		var groups []map[string]json.RawMessage
		if key == "balance_groups" {
			if err := json.Unmarshal([]byte(raw), &groups); err != nil {
				return err
			}
		} else {
			var legacy map[string]json.RawMessage
			if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
				return err
			}
			groups = []map[string]json.RawMessage{legacy}
		}
		changed := false
		for _, g := range groups {
			if _, ok := g["nodes"]; !ok {
				continue
			}
			var members []string
			if err := json.Unmarshal(g["nodes"], &members); err != nil {
				return err
			}
			kept := make([]string, 0, len(members))
			for _, id := range members {
				if !removed[id] {
					kept = append(kept, id)
				}
			}
			if len(kept) == len(members) {
				continue
			}
			changed = true
			g["nodes"], _ = json.Marshal(kept)
			var id string
			json.Unmarshal(g["id"], &id)
			if id != "" {
				var preferred string
				if err := tx.QueryRow("SELECT value FROM settings WHERE key=?", "group_preferred:"+id).Scan(&preferred); err != nil && err != sql.ErrNoRows {
					return err
				}
				if removed[preferred] {
					if _, err := tx.Exec("DELETE FROM settings WHERE key=?", "group_preferred:"+id); err != nil {
						return err
					}
				}
			}
		}
		if changed {
			var value []byte
			if key == "balance_groups" {
				value, _ = json.Marshal(groups)
			} else {
				value, _ = json.Marshal(groups[0])
			}
			if _, err := tx.Exec("UPDATE settings SET value=? WHERE key=?", string(value), key); err != nil {
				return err
			}
		}
	}
	return nil
}

func repairDeletedGroupMembers() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT substr(key,14) FROM settings WHERE substr(key,1,13)='node_deleted:' AND value='1' AND NOT EXISTS(SELECT 1 FROM nodes WHERE CAST(id AS TEXT)=substr(settings.key,14))")
	if err != nil {
		return err
	}
	removed := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		removed[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(removed) > 0 {
		if err = removeDeletedGroupMembers(tx, removed); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const nextNodeID = "(SELECT MAX(COALESCE(MAX(id),0),CAST(COALESCE((SELECT value FROM settings WHERE key='node_id_floor'),'0') AS INTEGER))+1 FROM nodes)"
