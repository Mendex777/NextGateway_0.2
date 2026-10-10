package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Rule struct {
	ValueCount                int
	TargetLabel               string
	Source                    string
	Disabled                  bool
	ID, Priority              int
	Name, Kind, Value, Target string
}
type Runtime struct {
	Groups []BalanceGroup

	Balance                                     bool
	AppliedNetwork                              GatewayNetwork
	State, Message, Updated, Action, ConfigHash string
	Gateway                                     bool
	Network                                     string
}
type Job struct {
	Groups []BalanceGroup `json:"groups,omitempty"`

	Network    GatewayNetwork `json:"network"`
	ID, Action string
	Config     map[string]any `json:"config,omitempty"`
	Gateway    bool           `json:"gateway"`
	DNS        string         `json:"dns,omitempty"`
	ConfigHash string         `json:"config_hash,omitempty"`
}

var jobLock sync.Mutex

func stateDir() string {
	if s := os.Getenv("NG_STATE"); s != "" {
		return s
	}
	return "/var/lib/ngpanel"
}
func readRuntime() Runtime {
	var s Runtime
	b, e := os.ReadFile(filepath.Join(stateDir(), "runtime.json"))
	if e == nil {
		json.Unmarshal(b, &s)
	}
	return s
}
func saveSetting(k, v string) error {
	_, e := db.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, v)
	return e
}
func allNodes() []Node {
	members := map[string]bool{}
	for _, id := range groupNodeIDs() {
		members[id] = true
	}
	rows, e := db.Query("SELECT n.id,n.source_id,name,host,port,transport,security,COALESCE(s.value,'{}'),n.uri,COALESCE(d.value,'0')='1' FROM nodes n LEFT JOIN settings s ON s.key='node_probe:' || n.id LEFT JOIN settings d ON d.key='source_disabled:' || n.source_id ORDER BY n.source_id,COALESCE((SELECT CAST(value AS INTEGER) FROM settings WHERE key='node_order:' || n.id),2147483647),n.id")
	if e != nil {
		return nil
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		var n Node
		var probe, raw string
		if rows.Scan(&n.ID, &n.SourceID, &n.Name, &n.Host, &n.Port, &n.Transport, &n.Security, &probe, &raw, &n.Disabled) == nil {
			json.Unmarshal([]byte(probe), &n.Probe)
			fillNodeDetails(&n, raw)
			n.BalanceMember = members[strconv.Itoa(n.ID)]
			out = append(out, n)
		}
	}
	rows.Close()
	return out
}
func allRules() []Rule {
	rows, e := db.Query("SELECT r.id,priority,name,kind,r.value,target,COALESCE(s.value,'0')='1',COALESCE(src.value,'') FROM rules r LEFT JOIN settings s ON s.key='rule_disabled:' || r.id LEFT JOIN settings src ON src.key='rule_source:' || r.id ORDER BY priority,r.id")
	if e != nil {
		return nil
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		if rows.Scan(&r.ID, &r.Priority, &r.Name, &r.Kind, &r.Value, &r.Target, &r.Disabled, &r.Source) == nil {
			out = append(out, r)
		}
	}
	rows.Close()
	for i := range out {
		out[i].TargetLabel = targetLabel(out[i].Target)
		if effectiveTarget(out[i].Target) != out[i].Target {
			out[i].TargetLabel += " — недоступен"
		}
		out[i].ValueCount = len(strings.FieldsFunc(out[i].Value, func(c rune) bool { return c == '\n' || c == ',' }))
	}
	return out
}
func validIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil && !strings.Contains(s, ":")
}

// A generated outbound has no arbitrary file paths, script hooks or raw JSON from the browser.
func vlessOutbound(raw string) (map[string]any, error) {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || u.Scheme != "vless" || u.User == nil || u.User.Username() == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("Некорректная VLESS-ссылка")
	}
	port, e := strconv.Atoi(u.Port())
	if e != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("Некорректный порт VLESS")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && ip.To4() == nil {
		return nil, fmt.Errorf("IPv6-серверы исключены из этой версии")
	}
	q := u.Query()
	transport := q.Get("type")
	if transport == "" {
		transport = "tcp"
	}
	if transport == "raw" {
		transport = "tcp"
	}
	if transport != "tcp" && transport != "ws" && transport != "grpc" && transport != "xhttp" {
		return nil, fmt.Errorf("Транспорт %s пока не поддерживается", transport)
	}
	sec := q.Get("security")
	if sec == "" {
		sec = "none"
	}
	if sec != "none" && sec != "tls" && sec != "reality" {
		return nil, fmt.Errorf("Неизвестный тип защиты VLESS")
	}
	if sec == "reality" && transport == "ws" {
		return nil, fmt.Errorf("REALITY несовместим с WebSocket")
	}
	if h := q.Get("headerType"); h != "" && h != "none" {
		return nil, fmt.Errorf("Маскировка headerType пока не поддерживается")
	}
	stream := map[string]any{"network": transport, "security": sec, "sockopt": map[string]any{"mark": 666, "domainStrategy": "UseIPv4"}}
	if sec == "reality" {
		if q.Get("pbk") == "" {
			return nil, fmt.Errorf("Отсутствует публичный ключ REALITY")
		}
		fp := q.Get("fp")
		if fp == "" {
			fp = "chrome"
		}
		stream["realitySettings"] = map[string]any{"serverName": q.Get("sni"), "fingerprint": fp, "publicKey": q.Get("pbk"), "mldsa65Verify": q.Get("pqv"), "shortId": q.Get("sid"), "spiderX": q.Get("spx")}
	}
	if sec == "tls" {
		tls := map[string]any{"serverName": q.Get("sni"), "allowInsecure": false}
		if q.Get("fp") != "" {
			tls["fingerprint"] = q.Get("fp")
		}
		if q.Get("pcs") != "" {
			tls["pinnedPeerCertSha256"] = q.Get("pcs")
		}
		if q.Get("alpn") != "" {
			tls["alpn"] = strings.Split(q.Get("alpn"), ",")
		}
		stream["tlsSettings"] = tls
	}
	switch transport {
	case "ws":
		path := q.Get("path")
		if path == "" {
			path = "/"
		}
		ws := map[string]any{"path": path}
		if q.Get("host") != "" {
			ws["headers"] = map[string]string{"Host": q.Get("host")}
		}
		stream["wsSettings"] = ws
	case "grpc":
		stream["grpcSettings"] = map[string]any{"authority": q.Get("authority"), "serviceName": q.Get("serviceName"), "multiMode": q.Get("mode") == "multi"}
	case "xhttp":
		xhttp, e := xhttpSettings(q)
		if e != nil {
			return nil, e
		}
		stream["xhttpSettings"] = xhttp
	}
	encryption := q.Get("encryption")
	if encryption == "" {
		encryption = "none"
	}
	if e := validateEncryption(encryption); e != nil {
		return nil, e
	}
	user := map[string]any{"id": u.User.Username(), "encryption": encryption}

	if flow := q.Get("flow"); flow != "" {
		if (flow != "xtls-rprx-vision" && flow != "xtls-rprx-vision-udp443") || transport != "tcp" || sec == "none" {
			return nil, fmt.Errorf("Несовместимый flow VLESS")
		}
		user["flow"] = flow
	}
	return map[string]any{"tag": "proxy", "protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": u.Hostname(), "port": port, "users": []any{user}}}}, "streamSettings": stream}, nil
}
func buildConfig() (map[string]any, error) {
	dns := bootstrapDNS()
	if !validIPv4(dns) {
		return nil, fmt.Errorf("DNS должен быть IPv4-адресом")
	}
	mode := effectiveTarget(setting("default_route"))
	rules := allRules()
	for i := range rules {
		rules[i].Target = effectiveTarget(rules[i].Target)
	}
	needProxy := mode == "proxy" || setting("dns_mode") == "proxy" && effectiveTarget("proxy") == "proxy"
	for _, r := range rules {
		if r.Disabled {
			continue
		}
		if r.Target == "proxy" {
			needProxy = true
		}
	}
	sock := map[string]any{"sockopt": map[string]any{"mark": 666}}
	out := []any{map[string]any{"tag": "direct", "protocol": "freedom", "settings": map[string]any{"domainStrategy": "UseIPv4"}, "streamSettings": sock}, map[string]any{"tag": "block", "protocol": "blackhole", "settings": map[string]any{}}, map[string]any{"tag": "dns-out", "protocol": "dns", "settings": map[string]any{"network": "udp", "address": dns, "port": 53}}}
	selectedHost := ""
	if needProxy {
		var raw string
		if e := db.QueryRow("SELECT uri FROM nodes WHERE id=?", setting("selected_node")).Scan(&raw); e != nil {
			return nil, fmt.Errorf("Выберите VPN-выход на странице подключений")
		}
		proxy, e := nodeOutbound(raw)
		if e != nil {
			return nil, e
		}
		out = append(out, proxy)
		u, _ := url.Parse(raw)
		selectedHost = u.Hostname()
	}
	assigned := map[string]bool{}
	bootstrapHosts := []string{}
	if selectedHost != "" && net.ParseIP(selectedHost) == nil {
		bootstrapHosts = append(bootstrapHosts, "full:"+selectedHost)
	}
	for _, r := range append(rules, Rule{Target: mode}) {
		if r.Disabled || !strings.HasPrefix(r.Target, "node:") {
			continue
		}
		id := strings.TrimPrefix(r.Target, "node:")
		if assigned[id] {
			continue
		}
		var raw string
		if db.QueryRow("SELECT uri FROM nodes WHERE id=?", id).Scan(&raw) != nil {
			return nil, fmt.Errorf("Подключение для правила %s отсутствует", r.Name)
		}
		proxy, err := nodeOutbound(raw)
		if err != nil {
			return nil, err
		}
		proxy["tag"] = "node-" + id
		out = append(out, proxy)
		assigned[id] = true
		u, _ := url.Parse(raw)
		if net.ParseIP(u.Hostname()) == nil {
			bootstrapHosts = append(bootstrapHosts, "full:"+u.Hostname())
		}
	}
	servers, dnsRoutes, err := dnsPolicy(dns, mode, rules, bootstrapHosts)
	if err != nil {
		return nil, err
	}
	routing := []any{map[string]any{"type": "field", "inboundTag": []string{"dns-in"}, "outboundTag": "dns-out"}, map[string]any{"type": "field", "inboundTag": []string{"dns-bootstrap"}, "outboundTag": "direct"}, map[string]any{"type": "field", "inboundTag": []string{"dns-upstream"}, "outboundTag": targetTag(dnsDefaultTarget(mode))}, map[string]any{"type": "field", "ip": []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}, "outboundTag": "direct"}}
	routing = append(routing, dnsRoutes...)
	for _, r := range rules {
		if r.Disabled {
			continue
		}
		field := "domain"
		if r.Kind == "ip" {
			field = "ip"
		}
		entry := map[string]any{"type": "field", "outboundTag": targetTag(r.Target)}
		if r.Kind != "device" {
			entry[field] = strings.FieldsFunc(r.Value, func(c rune) bool { return c == '\n' || c == ',' })
		}
		if r.Source != "" {
			entry["source"] = strings.Split(r.Source, "\n")
		}
		routing = append(routing, entry)
	}
	routing = append(routing, map[string]any{"type": "field", "network": "tcp,udp", "outboundTag": targetTag(mode)})
	config := map[string]any{"log": map[string]any{"loglevel": "warning"}, "dns": map[string]any{"queryStrategy": "UseIPv4", "disableFallbackIfMatch": true, "servers": servers}, "outbounds": out, "routing": map[string]any{"domainStrategy": "AsIs", "rules": routing}, "inbounds": []any{
		map[string]any{"tag": "tproxy-in", "listen": "0.0.0.0", "port": 7895, "protocol": "dokodemo-door", "settings": map[string]any{"network": "tcp,udp", "followRedirect": true}, "streamSettings": map[string]any{"sockopt": map[string]any{"tproxy": "tproxy"}}, "sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}},
		map[string]any{"tag": "dns-in", "listen": "0.0.0.0", "port": 1053, "protocol": "dokodemo-door", "settings": map[string]any{"network": "tcp,udp", "address": dns, "port": 53}},
		map[string]any{"tag": "test-socks", "listen": "127.0.0.1", "port": 1080, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": true}}}}
	if e := addGroups(config); e != nil {
		return nil, e
	}
	return config, nil
}
func enqueue(action string, config map[string]any) error {
	jobLock.Lock()
	defer jobLock.Unlock()
	j := Job{Groups: balanceGroups(), Network: gatewayNetwork(), ID: strconv.FormatInt(time.Now().UnixNano(), 10), Action: action, Config: config, Gateway: setting("gateway_enabled") == "1", DNS: bootstrapDNS()}
	for i := range j.Groups {
		active := []string{}
		for _, id := range j.Groups[i].Nodes {
			if nodeAvailable(id) {
				active = append(active, id)
			}
		}
		j.Groups[i].Nodes = active
	}
	if action == "network" || (action == "apply" && j.Gateway) {
		if e := saveNetwork(j.Network); e != nil {
			return e
		}
	}

	if config != nil {
		b, _ := json.Marshal(config)
		h := sha256.Sum256(b)
		j.ConfigHash = hex.EncodeToString(h[:])
	}
	b, e := json.Marshal(j)
	if e != nil {
		return e
	}
	dir := filepath.Join(stateDir(), "jobs")
	if _, e = os.Stat(filepath.Join(dir, "control.request")); e == nil {
		return fmt.Errorf("Предыдущее задание ещё выполняется")
	}
	temp, e := os.CreateTemp(dir, ".request-")
	if e != nil {
		return e
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, e = temp.Write(b); e != nil {
		temp.Close()
		return e
	}
	if e = temp.Close(); e != nil {
		return e
	}
	return os.Rename(name, filepath.Join(dir, "control.request"))
}
func validateRule(kind, value, target string) (string, error) {
	if !validTarget(target) {
		return "", fmt.Errorf("Некорректная цель правила")
	}
	items := strings.FieldsFunc(value, func(c rune) bool { return c == ',' || c == '\n' })
	if len(items) == 0 {
		return "", fmt.Errorf("Пустое правило")
	}
	for i, s := range items {
		s = strings.TrimSpace(s)
		if kind == "ip" {
			if strings.HasPrefix(s, "geoip:") {
				if e := validGeo(s, "geoip"); e != nil {
					return "", e
				}
				items[i] = s
				continue
			}
			if !validIPv4(s) {
				ip, _, e := net.ParseCIDR(s)
				if e != nil || ip.To4() == nil {
					return "", fmt.Errorf("Правило IP требует IPv4 или CIDR")
				}
			}
		} else if kind == "domain" {
			if strings.HasPrefix(s, "geosite:") {
				if e := validGeo(s, "geosite"); e != nil {
					return "", e
				}
				items[i] = s
				continue
			}
			if !strings.HasPrefix(s, "domain:") && !strings.HasPrefix(s, "full:") {
				s = "domain:" + s
			}
			host := strings.SplitN(s, ":", 2)[1]
			if host == "" || strings.ContainsAny(host, " /\\\t\r\x00") {
				return "", fmt.Errorf("Некорректный домен")
			}
		} else {
			return "", fmt.Errorf("Некорректный тип правила")
		}
		items[i] = s
	}
	return strings.Join(items, "\n"), nil
}
func controlAction(r *http.Request) (bool, string, error) {
	var e error
	msg := "Сохранено в панели"
	switch r.FormValue("action") {
	case "panel-update-check", "panel-update-install", "panel-update-rollback":
		err := requestPanelUpdate(strings.TrimPrefix(r.FormValue("action"), "panel-update-"))
		return true, "Задание обновления панели поставлено в очередь", err
	case "xray-update-check":
		message, err := checkCoreUpdate()
		return true, message, err
	case "setup-skip":
		return true, "Мастер скрыт. Все настройки доступны на главной странице.", saveSetting("setup_skipped", "1")
	case "device-discover":
		e = discoverDevices()
		msg = "Обнаружение запущено; обновите страницу через несколько секунд"
	case "device-save":
		e = saveDevice(r)
	case "source-probe", "probe-all":
		e = startBatch(r.FormValue("id"), r.FormValue("mode"), r.FormValue("request_id"))
		msg = "Проверка подписки запущена"
	case "probe-cancel":
		cancelBatch()
		msg = "Отмена проверки запрошена"
	case "node-probe":
		e = startProbe(r.FormValue("id"), r.FormValue("mode"), r.FormValue("request_id"))
		msg = "Проверка запущена"
	case "apply", "check":
		var c map[string]any
		c, e = buildConfig()
		if e == nil {
			e = enqueue(r.FormValue("action"), c)
		}
		msg = "Задание поставлено в очередь; результат отображается на странице состояния"
	case "start", "restart", "stop", "rollback", "network-confirm", "logs", "geodata", "dependencies":
		e = enqueue(r.FormValue("action"), nil)
		msg = "Задание поставлено в очередь"
	case "group-select":
		return true, "Узел группы выбран. Автовыбор продолжит работу при следующей проверке.", selectGroupNode(r.FormValue("group_id"), r.FormValue("node_id"))
	case "group-check":
		return true, "Проверка группы запущена; результат появится внутри группы.", startGroupCheck(r.FormValue("group_id"), r.FormValue("request_id"))
	case "balance-delete":
		e = deleteBalance(r.FormValue("group_id"))
	case "balance-settings":
		e = saveBalance(r)
		msg = "Группа сохранена. Выберите её выходом нужных маршрутов и примените конфигурацию"
	case "network-save", "network-save-apply":
		e = saveNetwork(GatewayNetwork{Interface: r.FormValue("interface"), Address: r.FormValue("address"), CIDR: r.FormValue("cidr"), Router: r.FormValue("router"), Mode: r.FormValue("mode"), SystemDNS: r.FormValue("system_dns")})
		msg = "Параметры сети сохранены; примените сеть ВМ и конфигурацию шлюза"
		if e == nil && r.FormValue("action") == "network-save-apply" {
			e = enqueue("network", nil)
			msg = "Сеть ВМ сохранена и отправлена на применение; затем подтвердите доступность панели"
		}
	case "network-detect":
		var n GatewayNetwork
		n, e = detectNetwork()
		if e == nil {
			e = saveNetwork(n)
		}
		msg = "Параметры определены по текущей сети; проверьте адрес роутера"
	case "network":
		e = enqueue("network", nil)
		msg = "Изменение сети ВМ запрошено; после применения подтвердите доступность панели"
	case "gateway-settings":
		dns := strings.TrimSpace(r.FormValue("dns"))
		if dns != "" && !validIPv4(dns) {
			return true, "", fmt.Errorf("Нужен IPv4 DNS")
		}
		dm := r.FormValue("dns_mode")
		if dm != "direct" && dm != "proxy" && dm != "rules" {
			return true, "", fmt.Errorf("Некорректный режим DNS")
		}
		directServers, err := normalizeDNSServers(r.FormValue("dns_direct_servers"), dns)
		if err != nil {
			return true, "", err
		}
		vpnServers, err := normalizeDNSServers(r.FormValue("dns_vpn_servers"), "https://1.1.1.1/dns-query")
		if err != nil {
			return true, "", err
		}
		tx, err := db.Begin()
		if err != nil {
			return true, "", err
		}
		defer tx.Rollback()
		for k, v := range map[string]string{"dns_direct": dns, "dns_mode": dm, "dns_direct_servers": directServers, "dns_vpn_servers": vpnServers} {
			if _, e = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, v); e != nil {
				return true, "", e
			}
		}
		e = tx.Commit()
	case "clear-vpn":
		e = saveSetting("selected_node", "")
	case "vpn-unavailable":
		value := r.FormValue("value")
		if value != "block" && value != "direct" {
			return true, "", fmt.Errorf("Некорректное поведение VPN")
		}
		e = saveSetting("vpn_unavailable", value)
	case "source-toggle":
		e = setSourceEnabled(r.FormValue("id"), r.FormValue("enabled") == "1")
	case "select-node":
		if nodeDisabled(r.FormValue("id")) {
			return true, "", fmt.Errorf("Подписка отключена")
		}
		var raw string
		e = db.QueryRow("SELECT uri FROM nodes WHERE id=?", r.FormValue("id")).Scan(&raw)
		if e != nil {
			return true, "", fmt.Errorf("Узел отсутствует")
		}
		if e == nil {
			if out, err := nodeOutbound(raw); err != nil {
				return true, "", err
			} else if err = validateXrayOutbound(out); err != nil {
				return true, "", err
			}
			e = saveSetting("selected_node", r.FormValue("id"))
		}
	case "manual-node":
		raw := strings.TrimSpace(r.FormValue("uri"))
		if _, e = nodeOutbound(raw); e != nil {
			return true, "", e
		}
		nodes, err := parseNodes(raw)
		if err != nil {
			return true, "", err
		}
		var sid int
		e = db.QueryRow("SELECT id FROM sources WHERE url='manual:' LIMIT 1").Scan(&sid)
		if e != nil {
			res, err := db.Exec("INSERT INTO sources(name,url,headers) VALUES('Ручные подключения','manual:','{}')")
			if err != nil {
				return true, "", err
			}
			id, _ := res.LastInsertId()
			sid = int(id)
		}
		for uri, n := range nodes {
			_, e = db.Exec("INSERT INTO nodes(id,source_id,uri,name,host,port,transport,security) VALUES("+nextNodeID+",?,?,?,?,?,?,?)", sid, uri, n.Name, n.Host, n.Port, n.Transport, n.Security)
		}
	case "rule-add", "rule-update":
		if err := validateRuleNode(r.FormValue("target")); err != nil {
			return true, "", err
		}
		var value string
		source, err := validateSource(r.FormValue("source"))
		if err != nil {
			return true, "", err
		}
		if r.FormValue("kind") == "device" {
			if source == "" {
				return true, "", fmt.Errorf("Для правила устройства укажите IP источника")
			}
			if target := r.FormValue("target"); !validTarget(target) {
				return true, "", fmt.Errorf("Некорректная цель правила")
			}
		} else {
			value, e = validateRule(r.FormValue("kind"), r.FormValue("value"), r.FormValue("target"))
		}
		if e == nil {
			name := strings.TrimSpace(r.FormValue("name"))
			if name == "" || len(name) > 200 {
				return true, "", fmt.Errorf("Название: от 1 до 200 символов")
			}
			tx, err := db.Begin()
			if err != nil {
				return true, "", err
			}
			defer tx.Rollback()
			var id int64
			if r.FormValue("action") == "rule-update" {
				if err = tx.QueryRow("SELECT id FROM rules WHERE id=?", r.FormValue("id")).Scan(&id); err == nil {
					_, err = tx.Exec("UPDATE rules SET name=?,kind=?,value=?,target=? WHERE id=?", name, r.FormValue("kind"), value, r.FormValue("target"), id)
				}
			} else {
				var res sql.Result
				res, err = tx.Exec("INSERT INTO rules(priority,name,kind,value,target) SELECT COALESCE(MAX(priority),0)+1,?,?,?,? FROM rules", name, r.FormValue("kind"), value, r.FormValue("target"))
				if err == nil {
					id, err = res.LastInsertId()
				}
			}
			if err == nil {
				_, err = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", fmt.Sprintf("rule_source:%d", id), source)
			}
			if err != nil {
				return true, "", err
			}
			e = tx.Commit()
		}
	case "rule-order", "rule-up", "rule-down":
		e = reorderRules(r.FormValue("action"), r.FormValue("id"), r.FormValue("order"), r.FormValue("before"))
	case "rule-bulk-target":
		target := r.FormValue("target")
		if !validTarget(target) {
			return true, "", fmt.Errorf("Некорректное исходящее подключение")
		}
		ids := strings.Split(r.FormValue("ids"), ",")
		if len(ids) == 0 || len(ids) > 10000 {
			return true, "", fmt.Errorf("Выберите правила")
		}
		tx, err := db.Begin()
		if err != nil {
			return true, "", err
		}
		defer tx.Rollback()
		for _, raw := range ids {
			id, err := strconv.Atoi(raw)
			if err != nil || id <= 0 {
				return true, "", fmt.Errorf("Некорректный номер правила")
			}
			result, err := tx.Exec("UPDATE rules SET target=? WHERE id=?", target, id)
			if err != nil {
				return true, "", err
			}
			count, err := result.RowsAffected()
			if err != nil || count != 1 {
				return true, "", fmt.Errorf("Правило %d больше не существует", id)
			}
		}
		e = tx.Commit()
		msg = fmt.Sprintf("Исходящее подключение изменено для %d правил", len(ids))
	case "rule-bulk-delete":
		ids := strings.Split(r.FormValue("ids"), ",")
		if len(ids) == 0 || len(ids) > 10000 {
			return true, "", fmt.Errorf("Выберите правила")
		}
		tx, err := db.Begin()
		if err != nil {
			return true, "", err
		}
		defer tx.Rollback()
		for _, raw := range ids {
			id, err := strconv.Atoi(raw)
			if err != nil || id <= 0 {
				return true, "", fmt.Errorf("Некорректный номер правила")
			}
			result, err := tx.Exec("DELETE FROM rules WHERE id=?", id)
			if err != nil {
				return true, "", err
			}
			count, err := result.RowsAffected()
			if err != nil || count != 1 {
				return true, "", fmt.Errorf("Правило %d больше не существует", id)
			}
			if _, err = tx.Exec("DELETE FROM settings WHERE key IN (?,?)", fmt.Sprintf("rule_disabled:%d", id), fmt.Sprintf("rule_source:%d", id)); err != nil {
				return true, "", err
			}
		}
		e = tx.Commit()
		msg = fmt.Sprintf("Удалено правил: %d", len(ids))
	case "rule-toggle":
		var id int
		if e = db.QueryRow("SELECT id FROM rules WHERE id=?", r.FormValue("id")).Scan(&id); e == nil {
			key := fmt.Sprintf("rule_disabled:%d", id)
			next := "1"
			if setting(key) == "1" {
				next = "0"
			}
			e = saveSetting(key, next)
		}
	case "rule-delete":
		var tx *sql.Tx
		tx, e = db.Begin()
		if e != nil {
			break
		}
		_, e = tx.Exec("DELETE FROM settings WHERE key IN ('rule_disabled:' || (SELECT id FROM rules WHERE id=?),'rule_source:' || (SELECT id FROM rules WHERE id=?))", r.FormValue("id"), r.FormValue("id"))
		if e == nil {
			_, e = tx.Exec("DELETE FROM rules WHERE id=?", r.FormValue("id"))
		}
		if e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
	case "node-delete":
		e = deleteConnections(r.FormValue("id"), false)
	default:
		return false, "", nil
	}
	return true, msg, e
}
