package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type routeTemplateRule struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	Target   string `json:"target"`
	Source   string `json:"source,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}
type routeTemplateExit struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}
type routeTemplate struct {
	Format  string              `json:"format"`
	Version int                 `json:"version"`
	Rules   []routeTemplateRule `json:"rules"`
	Exits   []routeTemplateExit `json:"outgoings"`
	Default string              `json:"default_route"`
}

func exportRoutes() routeTemplate {
	t := routeTemplate{Format: "ngpanel-routing", Version: 1, Rules: []routeTemplateRule{}, Exits: []routeTemplateExit{}, Default: setting("default_route")}
	aliases := map[string]string{}
	alias := func(target string) string {
		if target == "direct" || target == "proxy" || target == "block" {
			return target
		}
		if a, ok := aliases[target]; ok {
			return a
		}
		a := fmt.Sprintf("out-%d", len(aliases)+1)
		aliases[target] = a
		kind := "node"
		if strings.HasPrefix(target, "group:") {
			kind = "group"
		}
		t.Exits = append(t.Exits, routeTemplateExit{a, targetLabel(target), kind})
		return a
	}
	for _, r := range allRules() {
		t.Rules = append(t.Rules, routeTemplateRule{r.Name, r.Kind, r.Value, alias(r.Target), r.Source, r.Disabled})
	}
	t.Default = alias(t.Default)
	return t
}
func validateRouteTemplate(t *routeTemplate) error {
	if t.Format != "ngpanel-routing" || t.Version != 1 {
		return fmt.Errorf("Нужен шаблон ngpanel-routing версии 1; экспортируйте правила из NGPanel")
	}
	if len(t.Rules) > 1000 || len(t.Exits) > 1000 {
		return fmt.Errorf("В шаблоне допускается до 1000 правил и выходов")
	}
	keys := map[string]bool{"direct": true, "proxy": true, "block": true}
	for _, e := range t.Exits {
		if e.Key == "" || keys[e.Key] || len(e.Key) > 100 || len(e.Label) > 500 || (e.Kind != "node" && e.Kind != "group") {
			return fmt.Errorf("Некорректный список исходящих")
		}
		keys[e.Key] = true
	}
	if !keys[t.Default] {
		return fmt.Errorf("Неизвестный маршрут по умолчанию")
	}
	for i, r := range t.Rules {
		if strings.TrimSpace(r.Name) == "" || len([]rune(r.Name)) > 200 || len(r.Value) > 65536 || len(r.Source) > 8192 || !keys[r.Target] {
			return fmt.Errorf("Некорректное правило %d", i+1)
		}
		if r.Kind != "domain" && r.Kind != "ip" && r.Kind != "device" {
			return fmt.Errorf("Некорректный тип правила %d", i+1)
		}
		source, e := validateSource(r.Source)
		if e != nil {
			return e
		}
		t.Rules[i].Source = source
		if r.Kind == "device" {
			if source == "" {
				return fmt.Errorf("Укажите IP для правила устройства %s", r.Name)
			}
			t.Rules[i].Value = ""
		} else {
			v, e := validateRule(r.Kind, r.Value, "direct")
			if e != nil {
				return fmt.Errorf("%s: %w", r.Name, e)
			}
			t.Rules[i].Value = v
		}
	}
	return nil
}
func githubRouteURL(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return "", fmt.Errorf("Укажите HTTPS-ссылку GitHub на JSON шаблона")
	}
	switch u.Hostname() {
	case "github.com":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 5 || parts[2] != "blob" {
			return "", fmt.Errorf("Нужна ссылка GitHub на файл, либо raw.githubusercontent.com")
		}
		u.Host = "raw.githubusercontent.com"
		u.Path = "/" + strings.Join(append(parts[:2], parts[3:]...), "/")
	case "raw.githubusercontent.com", "gist.githubusercontent.com":
	default:
		return "", fmt.Errorf("Поддерживаются github.com, raw.githubusercontent.com и gist.githubusercontent.com")
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
func importRoutes(t routeTemplate, mapping, sources map[string]string, replace, defaultRoute bool, before string) (int, error) {
	if e := validateRouteTemplate(&t); e != nil {
		return 0, e
	}
	if before != orderString(allRules()) {
		return 0, fmt.Errorf("Правила изменились. Обновите страницу и повторите импорт")
	}
	resolve := func(key string) (string, error) {
		if key == "direct" || key == "proxy" || key == "block" {
			return key, nil
		}
		target := mapping[key]
		if target == "skip" {
			return target, nil
		}
		if target == "" {
			return "", fmt.Errorf("Сопоставьте все исходящие шаблона")
		}
		if e := validateRuleNode(target); e != nil {
			return "", e
		}
		return target, nil
	}
	for _, exit := range t.Exits {
		if _, e := resolve(exit.Key); e != nil {
			return 0, e
		}
	}
	prepared := []routeTemplateRule{}
	for _, r := range t.Rules {
		target, e := resolve(r.Target)
		if e != nil {
			return 0, e
		}
		if target == "skip" {
			continue
		}
		r.Target = target
		if mapped, ok := sources[r.Source]; ok && r.Source != "" {
			r.Source = mapped
		}
		source, e := validateSource(r.Source)
		if e != nil {
			return 0, e
		}
		r.Source = source
		if r.Kind == "device" && source == "" {
			return 0, fmt.Errorf("IP устройства не может быть пустым")
		}
		prepared = append(prepared, r)
	}
	route := ""
	if defaultRoute {
		var e error
		route, e = resolve(t.Default)
		if e != nil {
			return 0, e
		}
	}
	tx, e := db.Begin()
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	var currentOrder string
	if e = tx.QueryRow("SELECT COALESCE(group_concat(id,','),'') FROM (SELECT id FROM rules ORDER BY priority,id)").Scan(&currentOrder); e != nil {
		return 0, e
	}
	if currentOrder != before {
		return 0, fmt.Errorf("Правила изменились. Обновите страницу и повторите импорт")
	}
	if replace {
		if _, e = tx.Exec("DELETE FROM settings WHERE key LIKE 'rule_disabled:%' OR key LIKE 'rule_source:%'"); e != nil {
			return 0, e
		}
		if _, e = tx.Exec("DELETE FROM rules"); e != nil {
			return 0, e
		}
	}
	for _, r := range prepared {
		result, e := tx.Exec("INSERT INTO rules(priority,name,kind,value,target) SELECT COALESCE(MAX(priority),0)+1,?,?,?,? FROM rules", r.Name, r.Kind, r.Value, r.Target)
		if e != nil {
			return 0, e
		}
		id, e := result.LastInsertId()
		if e != nil {
			return 0, e
		}
		for key, value := range map[string]string{fmt.Sprintf("rule_source:%d", id): r.Source, fmt.Sprintf("rule_disabled:%d", id): fmt.Sprint(map[bool]int{true: 1, false: 0}[r.Disabled])} {
			if _, e = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value); e != nil {
				return 0, e
			}
		}
	}
	if route != "" && route != "skip" {
		if _, e = tx.Exec("INSERT INTO settings(key,value) VALUES('default_route',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", route); e != nil {
			return 0, e
		}
	}
	return len(prepared), tx.Commit()
}
func routesTemplateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "GET" {
		w.Header().Set("Content-Disposition", "attachment; filename=ngpanel-routing.json")
		json.NewEncoder(w).Encode(exportRoutes())
		return
	}
	fail := func(e error) { http.Error(w, e.Error(), 400) }
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	if r.Header.Get("Origin") != "http://"+r.Host && r.Header.Get("Origin") != "https://"+r.Host {
		http.Error(w, "Invalid origin", 403)
		return
	}
	var req struct {
		Operation string            `json:"operation"`
		URL       string            `json:"url"`
		Template  routeTemplate     `json:"template"`
		Mapping   map[string]string `json:"mapping"`
		Sources   map[string]string `json:"sources"`
		Replace   bool              `json:"replace"`
		Default   bool              `json:"default"`
		Before    string            `json:"before"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
		fail(fmt.Errorf("Некорректный JSON"))
		return
	}
	if req.Operation == "preview" && req.URL != "" {
		address, e := githubRouteURL(req.URL)
		if e != nil {
			fail(e)
			return
		}
		client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return fmt.Errorf("Слишком много перенаправлений")
			}
			_, e := githubRouteURL(next.URL.String())
			return e
		}}
		request, e := http.NewRequestWithContext(r.Context(), "GET", address, nil)
		if e != nil {
			fail(e)
			return
		}
		response, e := client.Do(request)
		if e != nil {
			fail(fmt.Errorf("Не удалось загрузить шаблон GitHub"))
			return
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			fail(fmt.Errorf("GitHub вернул HTTP %d", response.StatusCode))
			return
		}
		raw, e := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
		if e != nil || len(raw) > 16<<20 {
			fail(fmt.Errorf("Шаблон превышает 16 МБ"))
			return
		}
		if e = json.Unmarshal(raw, &req.Template); e != nil {
			fail(fmt.Errorf("Файл GitHub не является шаблоном JSON"))
			return
		}
	}
	if e := validateRouteTemplate(&req.Template); e != nil {
		fail(e)
		return
	}
	switch req.Operation {
	case "preview":
		json.NewEncoder(w).Encode(req.Template)
	case "import":
		count, e := importRoutes(req.Template, req.Mapping, req.Sources, req.Replace, req.Default, req.Before)
		if e != nil {
			fail(e)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "count": count})
	default:
		fail(fmt.Errorf("Неизвестная операция"))
	}
}
