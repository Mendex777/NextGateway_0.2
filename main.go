package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var panelVersion = "v0.2.0"
var panelCommit = "dev"
var db *sql.DB
var view *template.Template
var viewOnce sync.Once

func pageTemplate() *template.Template {
	viewOnce.Do(func() { view = template.Must(template.ParseFiles("web.html")) })
	return view
}

type Source struct {
	ProviderTitle, ProviderMessage, Usage, Limit, Expires, NextUpdate string
	Nodes                                                             []Node
	HasSelected                                                       bool
	Interval                                                          int
	ID                                                                int
	Name, URL, Headers, Updated, Error                                string
	Count                                                             int
}
type Node struct {
	BalanceMember                                        bool
	Compatibility, Encryption, SNI, Flow, Path, Protocol string
	SourceID                                             int
	Probe                                                ProbeResult
	ID                                                   int
	Name, Host, Transport, Security                      string
	Port                                                 string
}
type Page struct {
	SourceCount, NodeCount, RuleCount, DeviceCount                        int
	GatewayReady                                                          bool
	Components                                                            []ComponentStatus
	Readiness                                                             string
	Wizard                                                                bool
	Groups                                                                []BalanceGroup
	EditGroup                                                             *BalanceGroup
	Network, DetectedNetwork                                              GatewayNetwork
	NetworkError                                                          string
	OperationAction, OperationSince                                       string
	PanelVersion, PanelCommit                                             string
	PanelUpdate                                                           PanelUpdateStatus
	Devices                                                               []Device
	EditDevice                                                            *Device
	DeviceDiscovery                                                       string
	SelectedNode                                                          *Node
	EditSource                                                            *Source
	RuleOrder                                                             string
	RouteCheck                                                            *RouteCheck
	EditRule                                                              *Rule
	GeoDetail                                                             *GeoCategory
	GeoFilter                                                             string
	GeoOffset, GeoNext                                                    int
	GeoQuery, GeoError                                                    string
	GeoResults                                                            []GeoCategory
	GatewayHealth                                                         string
	Tab, Message, Mode, Version, Service, Routes, Memory, Uptime, Install string
	Sources                                                               []Source
	Nodes                                                                 []Node
	Rules                                                                 []Rule
	Runtime                                                               Runtime
	DNSDirectServers, DNSVPNServers                                       string
	DNS, DNSMode, Gateway, Selected                                       string
	Pending                                                               bool
	ConfigError                                                           string
}

func command(name string, args ...string) string {
	b, e := exec.Command(name, args...).CombinedOutput()
	if e != nil && len(b) == 0 {
		return "Недоступно"
	}
	return strings.TrimSpace(string(b))
}
func setting(key string) string {
	var v string
	db.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&v)
	return v
}
func sources() []Source {
	rows, e := db.Query("SELECT id,name,url,headers,updated,error,(SELECT count(*) FROM nodes WHERE source_id=sources.id) FROM sources ORDER BY id")
	if e != nil {
		return nil
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		var s Source
		if rows.Scan(&s.ID, &s.Name, &s.URL, &s.Headers, &s.Updated, &s.Error, &s.Count) == nil {
			out = append(out, s)
		}
	}
	return out
}
func parseNodes(body string) (map[string]Node, error) {
	body = strings.TrimSpace(body)
	if !strings.Contains(body, "://") {
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if b, e := enc.DecodeString(strings.Join(strings.Fields(body), "")); e == nil {
				body = string(b)
				break
			}
		}
	}
	if strings.HasPrefix(body, "[") || strings.HasPrefix(body, "{") {
		return nil, fmt.Errorf("Сервер вернул JSON-профиль вместо VLESS-ссылок. Выберите User-Agent для текстовой/Base64 подписки (например v2rayNG)")
	}
	out := map[string]Node{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		u, e := url.Parse(line)
		if e != nil || (u.Scheme != "vless" && u.Scheme != "hy2" && u.Scheme != "hysteria2") {
			continue
		}
		if u.User == nil || u.User.Username() == "" || u.Hostname() == "" || (u.Port() == "" && u.Scheme == "vless") {
			return nil, fmt.Errorf("Некорректная VLESS-ссылка")
		}
		n := Node{Name: u.Fragment, Host: u.Hostname(), Port: u.Port(), Transport: u.Query().Get("type"), Security: u.Query().Get("security")}
		if n.Name == "" {
			n.Name = n.Host
		}
		if n.Transport == "" {
			n.Transport = "tcp"
		}
		if u.Scheme != "vless" {
			n.Transport = "hysteria2"
			n.Security = "tls"
			if n.Port == "" {
				n.Port = "443"
			}
		}
		out[line] = n
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("Ссылки VLESS / Hysteria 2 не найдены; поддерживаются текст и Base64")
	}
	return out, nil
}
func refresh(id string) error {
	if !subscriptionLock.TryLock() {
		return fmt.Errorf("Обновление подписки уже выполняется")
	}
	defer subscriptionLock.Unlock()
	var raw, headers string
	if e := db.QueryRow("SELECT url,headers FROM sources WHERE id=?", id).Scan(&raw, &headers); e != nil {
		return e
	}
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return fmt.Errorf("Нужен HTTP(S) URL")
	}
	req, e := http.NewRequest("GET", raw, nil)
	if e != nil {
		return fmt.Errorf("Некорректный URL")
	}
	req.Header.Set("User-Agent", "NGPanel/0.1")
	var h map[string]string
	if e = json.Unmarshal([]byte(headers), &h); e != nil {
		return fmt.Errorf("Заголовки должны быть JSON-объектом")
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	client := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("Слишком много перенаправлений")
		}
		if r.URL.Host != via[0].URL.Host {
			return fmt.Errorf("Перенаправление на другой сервер запрещено")
		}
		return nil
	}}
	resp, e := client.Do(req)
	if e != nil {
		return fmt.Errorf("Не удалось получить подписку; проверьте URL и сеть")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Сервер подписки вернул HTTP %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if e != nil {
		return fmt.Errorf("Ошибка чтения подписки")
	}
	if len(b) > 4*1024*1024 {
		return fmt.Errorf("Подписка превышает 4 МБ")
	}
	nodes, e := parseNodes(string(b))
	if e != nil {
		return e
	}
	referenced := referencedNodes()
	selected := setting("selected_node")
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	previous := map[string]int{}
	rows, e := tx.Query("SELECT id,uri FROM nodes WHERE source_id=?", id)
	if e != nil {
		return e
	}
	for rows.Next() {
		var nid int
		var uri string
		if e = rows.Scan(&nid, &uri); e != nil {
			rows.Close()
			return e
		}
		previous[canonicalURI(uri)] = nid
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	for raw, n := range nodes {
		key := canonicalURI(raw)
		if nid, ok := previous[key]; ok {
			_, e = tx.Exec("UPDATE nodes SET uri=?,name=?,host=?,port=?,transport=?,security=? WHERE id=?", raw, n.Name, n.Host, n.Port, n.Transport, n.Security, nid)
			delete(previous, key)
		} else {
			_, e = tx.Exec("INSERT INTO nodes(source_id,uri,name,host,port,transport,security) VALUES(?,?,?,?,?,?,?)", id, raw, n.Name, n.Host, n.Port, n.Transport, n.Security)
		}
		if e != nil {
			return e
		}
	}
	for _, nid := range previous {
		if fmt.Sprint(nid) == selected || referenced[fmt.Sprint(nid)] {
			_, e = tx.Exec("UPDATE nodes SET name=CASE WHEN name LIKE ? THEN name ELSE name || ? END WHERE id=?", "% [исчез из подписки]", " [исчез из подписки]", nid)
			if e != nil {
				return e
			}
			continue
		}
		if _, e = tx.Exec("DELETE FROM nodes WHERE id=?", nid); e != nil {
			return e
		}
	}
	if _, e = tx.Exec("UPDATE sources SET updated=?,error='' WHERE id=?", time.Now().UTC().Format(time.RFC3339), id); e != nil {
		return e
	}
	info, _ := json.Marshal(subscriptionInfo(resp.Header))
	if _, e = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "sub_info:"+id, string(info)); e != nil {
		return e
	}
	return tx.Commit()
}
func handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'")
	if r.URL.Path == "/configuration-status" && r.Method == http.MethodGet {
		pending, configError := configurationState()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"pending": pending, "config_error": configError, "initialized": readRuntime().ConfigHash != ""})
		return
	}
	if r.URL.Path == "/operation-status" && r.Method == http.MethodGet {
		operationHandler(w, r)
		return
	}
	if r.URL.Path == "/panel-update-status" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(readPanelUpdate())
		return
	}
	if r.URL.Path == "/health" && r.Method == http.MethodGet {
		if e := db.Ping(); e != nil {
			http.Error(w, "Database unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"version": panelVersion, "commit": panelCommit})
		return
	}
	if r.URL.Path == "/backup" {
		backupHandler(w, r)
		return
	}
	if r.URL.Path == "/ui.js" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		http.ServeFile(w, r, "ui.js")
		return
	}
	if r.URL.Path == "/ui.css" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write([]byte(panelCSS))
		return
	}
	if r.URL.Path == "/group-check-status" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(groupCheckStatus(r.URL.Query().Get("id")))
		return
	}
	if r.URL.Path == "/balance-status" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(readGroupStatus(r.URL.Query().Get("id")))
		return
	}
	if r.URL.Path == "/probe-status" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(probeStatus())
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/action" {
		http.NotFound(w, r)
		return
	}
	if r.Method == "POST" {
		origin := r.Header.Get("Origin")
		u, e := url.Parse(origin)
		if e != nil || origin == "" || u.Host != r.Host {
			http.Error(w, "Invalid origin", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
		if e := r.ParseForm(); e != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		started := time.Now().UTC().Format(time.RFC3339Nano)
		msg := "Сохранено"
		handled, controlMessage, controlErr := controlAction(r)
		if handled {
			msg = controlMessage
			e = controlErr
		} else {
			switch r.FormValue("action") {
			case "install":
				f, err := os.OpenFile("/var/lib/ngpanel/jobs/install.request", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				e = err
				if err == nil {
					f.Close()
				}
				msg = "Установка запрошена. Обновите страницу через несколько секунд."
			case "settings":
				msg = "Маршрут сохранён. Нажмите «Применить изменения», чтобы изменить трафик."
				mode := r.FormValue("mode")
				if err := validateRuleNode(mode); err != nil {
					http.Error(w, "Invalid mode", 400)
					return
				}
				_, e = db.Exec("INSERT INTO settings(key,value) VALUES('default_route',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", mode)
			case "source", "source-update":
				var h map[string]string
				raw := r.FormValue("url")
				pu, pe := url.Parse(raw)
				if pe != nil || pu.Hostname() == "" || (pu.Scheme != "http" && pu.Scheme != "https") {
					http.Error(w, "Invalid URL", 400)
					return
				}
				if json.Unmarshal([]byte(r.FormValue("headers")), &h) != nil {
					http.Error(w, "Headers must be JSON", 400)
					return
				}
				e = saveSource(r)
			case "refresh":
				e = refreshRecorded(r.FormValue("id"))
				if e != nil {
					db.Exec("UPDATE sources SET error=? WHERE id=?", e.Error(), r.FormValue("id"))
				}
				msg = "Подписка импортирована; Xray не изменён"
			case "delete":
				if sourceProtected(r.FormValue("id")) {
					e = fmt.Errorf("Подписка содержит выбранный VPN или участника группы/правила; сначала измените настройки")
					break
				}
				res, err := db.Exec("DELETE FROM sources WHERE id=? AND NOT EXISTS (SELECT 1 FROM nodes n JOIN rules r ON r.target='node:' || n.id WHERE n.source_id=sources.id)", r.FormValue("id"))
				e = err
				if e == nil {
					n, _ := res.RowsAffected()
					if n == 0 {
						e = fmt.Errorf("Подключения источника используются правилами или источник уже удалён; сначала измените правила")
					}
				}
			default:
				http.Error(w, "Unknown action", 400)
				return
			}
		}
		if e != nil {
			msg = "Ошибка: " + e.Error()
		}
		if r.Header.Get("Accept") == "application/json" && (r.FormValue("action") == "settings" || r.FormValue("action") == "rule-order" || r.FormValue("action") == "rule-toggle" || r.FormValue("action") == "group-select" || r.FormValue("action") == "group-check" || r.FormValue("action") == "install" || r.FormValue("action") == "dependencies" || r.FormValue("action") == "geodata") {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			if e != nil {
				w.WriteHeader(http.StatusBadRequest)
			}
			result := map[string]any{"ok": e == nil, "message": msg, "since": started}
			if e == nil && r.FormValue("action") == "rule-toggle" {
				id, _ := strconv.Atoi(r.FormValue("id"))
				result["enabled"] = setting(fmt.Sprintf("rule_disabled:%d", id)) != "1"
			}
			pending, configError := configurationState()
			result["pending"], result["config_error"] = pending, configError
			result["initialized"] = readRuntime().ConfigHash != ""
			json.NewEncoder(w).Encode(result)
			return
		}
		redirect := "/?tab=" + url.QueryEscape(r.FormValue("tab")) + "&message=" + url.QueryEscape(msg)
		if e == nil && operationKind(r.FormValue("action")) != "" {
			redirect += "&operation=" + url.QueryEscape(r.FormValue("action")) + "&since=" + url.QueryEscape(started)
		}
		http.Redirect(w, r, redirect, 303)
		return
	}
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	p := Page{Tab: r.URL.Query().Get("tab"), Message: r.URL.Query().Get("message"), Mode: setting("default_route"), Runtime: readRuntime(), DNS: setting("dns_direct"), DNSMode: setting("dns_mode"), Gateway: setting("gateway_enabled"), Selected: setting("selected_node")}
	p.Network = gatewayNetwork()
	if detected, e := detectNetwork(); e == nil {
		p.DetectedNetwork = detected
	} else {
		p.NetworkError = e.Error()
	}
	p.Groups = balanceGroups()

	p.OperationAction, p.OperationSince = r.URL.Query().Get("operation"), r.URL.Query().Get("since")
	p.PanelVersion, p.PanelCommit = panelVersion, panelCommit
	p.PanelUpdate = readPanelUpdate()
	p.DNSDirectServers = setting("dns_direct_servers")
	if p.DNSDirectServers == "" {
		p.DNSDirectServers = p.DNS
	}
	p.DNSVPNServers = setting("dns_vpn_servers")
	if p.DNSVPNServers == "" {
		p.DNSVPNServers = "https://1.1.1.1/dns-query"
	}
	if p.Tab == "" {
		p.Tab = "status"
	}
	p.Pending, p.ConfigError = configurationState()

	if p.Tab == "status" {
		if p.Runtime.Gateway {
			policy := command("ip", "-4", "rule")
			routes := command("ip", "-4", "route", "show", "table", "100")
			if !strings.Contains(policy, "fwmark 0x1 lookup 100") || !strings.Contains(routes, "local default dev lo") {
				p.GatewayHealth = "Отсутствует правило или маршрут TPROXY. Примените конфигурацию для восстановления."
			}
		}
		p.Version = command("xray", "version")
		p.Service = command("systemctl", "is-active", "xray")
		p.Routes = command("ip", "route")
		p.Memory = command("free", "-h")
		p.Uptime = command("uptime", "-p")
		p.Components = componentOverview(p)
		p.Readiness = overallReadiness(p)
		p.GatewayReady = p.Runtime.Gateway && strings.TrimSpace(p.Service) == "active" && p.GatewayHealth == ""
		db.QueryRow("SELECT count(*) FROM sources").Scan(&p.SourceCount)
		db.QueryRow("SELECT count(*) FROM nodes").Scan(&p.NodeCount)
		p.RuleCount = len(allRules())
		p.DeviceCount = len(devices())
		p.Wizard = r.URL.Query().Get("setup") == "1" || (p.Runtime.ConfigHash == "" && setting("setup_skipped") != "1")
		if b, e := os.ReadFile("/var/lib/ngpanel/install-status"); e == nil {
			p.Install = string(b)
		}
	}
	if p.Tab == "nodes" {
		p.Tab = "subscriptions"
	}
	if p.Tab == "subscriptions" {
		p.Nodes = allNodes()
		p.Sources = sources()
		for i := range p.Groups {
			g := &p.Groups[i]
			g.Members = balanceMemberViews(BalanceSettings{Nodes: g.Nodes}, "", p.Nodes, p.Sources, "")
			if g.ID == r.URL.Query().Get("group") {
				p.EditGroup = g
			}
		}
		for i := range p.Sources {
			for _, n := range p.Nodes {
				if n.SourceID == p.Sources[i].ID {
					p.Sources[i].Nodes = append(p.Sources[i].Nodes, n)
					if fmt.Sprint(n.ID) == p.Selected {
						p.Sources[i].HasSelected = true
						copy := n
						p.SelectedNode = &copy
					}
				}
			}
			p.Sources[i].Interval, _ = strconv.Atoi(setting(fmt.Sprintf("sub_interval:%d", p.Sources[i].ID)))
			fillSourceInfo(&p.Sources[i], time.Now())
			if fmt.Sprint(p.Sources[i].ID) == r.URL.Query().Get("edit") {
				copy := p.Sources[i]
				p.EditSource = &copy
			}
		}
	}
	if p.Tab == "nodes" {
		p.Nodes = allNodes()
	}
	if p.Tab == "devices" {
		p.Devices = devices()
		p.DeviceDiscovery = setting("device_discovery")
		for _, d := range p.Devices {
			if d.IP == r.URL.Query().Get("edit") {
				copy := d
				p.EditDevice = &copy
			}
		}
	}
	if p.Tab == "routing" || p.Tab == "devices" {
		p.Rules = allRules()
		p.Sources = sources()
		p.Nodes = allNodes()
		for i := range p.Sources {
			for _, n := range p.Nodes {
				if n.SourceID == p.Sources[i].ID {
					p.Sources[i].Nodes = append(p.Sources[i].Nodes, n)
				}
			}
		}
		p.Devices = devices()
		p.RuleOrder = orderString(p.Rules)
		p.GeoQuery = r.URL.Query().Get("q")
		p.GeoResults = findGeo(p.GeoQuery)
		p.GeoError = geoError
		for _, rule := range p.Rules {
			if fmt.Sprint(rule.ID) == r.URL.Query().Get("edit") {
				copy := rule
				p.EditRule = &copy
			}
		}
		p.GeoFilter = r.URL.Query().Get("filter")
		p.GeoOffset, p.GeoNext, p.GeoDetail = geoDetail(r.URL.Query().Get("category"), p.GeoFilter, r.URL.Query().Get("offset"))
		if r.URL.Query().Get("test") == "1" {
			c := checkRoute(r.URL.Query().Get("domain"), r.URL.Query().Get("address"), r.URL.Query().Get("source"), p.Rules, p.Mode)
			explainDNS(&c)
			p.RouteCheck = &c
		}
	}
	if p.Tab == "diagnostics" {
		if b, e := os.ReadFile(filepath.Join(stateDir(), "xray-log")); e == nil {
			p.Service = string(b)
		}
		p.Routes = command("ip", "rule")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if e := pageTemplate().Execute(w, p); e != nil {
		log.Print(e)
	}
}
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("NGPanel %s %s\n", panelVersion, panelCommit)
		return
	}
	pageTemplate()
	path := os.Getenv("NG_DB")
	if path == "" {
		path = "panel.db"
	}
	var e error
	db, e = sql.Open("sqlite3", path+"?_foreign_keys=on&_busy_timeout=5000")
	if e != nil {
		log.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sources(id INTEGER PRIMARY KEY,name TEXT NOT NULL,url TEXT NOT NULL,headers TEXT NOT NULL,updated TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '');
 CREATE TABLE IF NOT EXISTS nodes(id INTEGER PRIMARY KEY,source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,uri TEXT NOT NULL,name TEXT NOT NULL,host TEXT NOT NULL,port TEXT NOT NULL,transport TEXT NOT NULL,security TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS rules(id INTEGER PRIMARY KEY,priority INTEGER NOT NULL,name TEXT NOT NULL,kind TEXT NOT NULL,value TEXT NOT NULL,target TEXT NOT NULL);
 INSERT OR IGNORE INTO settings VALUES('default_route','direct'),('dns_direct','1.1.1.1'),('dns_mode','direct'),('gateway_enabled','0'),('selected_node','');`)
	if e != nil {
		log.Fatal(e)
	}
	if setting("balance_groups") == "" {
		if e := saveGroups(balanceGroups()); e != nil {
			log.Fatal(e)
		}
	}
	addr := os.Getenv("NG_LISTEN")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	if _, _, e = net.SplitHostPort(addr); e != nil {
		log.Fatal(e)
	}
	log.Printf("NGPanel listening on %s", addr)
	go subscriptionWorker()
	go groupWorker()
	s := http.Server{Addr: addr, Handler: http.HandlerFunc(handler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(s.ListenAndServe())
}

// Compare the saved configuration with the running gateway for every page and AJAX action.
func configurationState() (bool, string) {
	c, err := buildConfig()
	if err != nil {
		return true, err.Error()
	}
	b, _ := json.Marshal(c)
	hash := sha256.Sum256(b)
	runtime := readRuntime()
	pending := hex.EncodeToString(hash[:]) != runtime.ConfigHash || groupPolicyPending(c, runtime.Groups) || (setting("gateway_enabled") == "1") != runtime.Gateway || (runtime.Gateway && gatewayNetwork() != runtime.AppliedNetwork)
	return pending, ""
}
