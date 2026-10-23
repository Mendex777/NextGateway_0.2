package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type ServiceCheck struct{ ID, Name, State, Summary, Expected, Actual string }
type ServiceHealth struct {
	Host    *statusHostSnapshot `json:"Host,omitempty"`
	Updated time.Time
	Checks  []ServiceCheck
	Message string
}
type healthEvidence struct {
	XrayPID                                                                 int
	ActiveSince                                                             uint64
	Runtime                                                                 Runtime
	Config, NFT, Baseline, Addresses, Routes, Rules, LocalRoutes            []byte
	Active, Version, Listeners, Forward, Filter, InterfaceFilter, Autostart string
	Geo                                                                     map[string]string
	DNSOK                                                                   bool
	ConfigValid                                                             bool
	ConfigTested                                                            time.Time
}

func healthCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}
func healthJSON(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func healthHash(raw []byte) string {
	v := healthJSON(raw)
	if v == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// Rule order and chain hooks matter; handles and live counters do not.
func canonicalHealthNFT(raw []byte) string {
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return ""
	}
	entries, ok := root["nftables"].([]any)
	if !ok {
		return ""
	}
	var clean func(any) any
	clean = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			r := map[string]any{}
			for k, x := range t {
				if k != "handle" && k != "packets" && k != "bytes" {
					r[k] = clean(x)
				}
			}
			return r
		case []any:
			r := []any{}
			for _, x := range t {
				r = append(r, clean(x))
			}
			return r
		}
		return v
	}
	out := []any{}
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if m["metainfo"] != nil {
			continue
		}
		out = append(out, clean(m))
	}
	if len(out) == 0 {
		return ""
	}
	b, _ := json.Marshal(out)
	return string(b)
}
func healthRows(raw []byte) []map[string]any {
	var rows []map[string]any
	_ = json.Unmarshal(raw, &rows)
	return rows
}
func buildServiceHealth(e healthEvidence) ServiceHealth {
	h := ServiceHealth{Updated: time.Now().UTC(), Checks: []ServiceCheck{}}
	add := func(id, name, state, summary, expected, actual string) {
		h.Checks = append(h.Checks, ServiceCheck{id, name, state, summary, expected, actual})
	}
	good := func(v bool) string {
		if v {
			return "good"
		}
		return "bad"
	}
	version := strings.Fields(e.Version)
	v := "Версия недоступна"
	if len(version) > 1 && version[0] == "Xray" {
		v = version[1]
	}
	running := strings.TrimSpace(e.Active) == "active"
	summary := "Остановлен"
	if running {
		summary = "Работает · " + v
	}
	add("xray", "Xray", good(running), summary, "Служба xray: active", strings.TrimSpace(e.Active)+" · "+v)
	var config struct {
		Inbounds []struct {
			Tag, Protocol, Listen string
			Port                  int
			Settings              struct {
				Network        string
				FollowRedirect bool
			}
			StreamSettings struct{ Sockopt struct{ Tproxy string } }
		}
		Routing struct {
			Rules []struct{ Domain, IP []string }
		}
		DNS struct{ Servers []any }
	}
	parsed := json.Unmarshal(e.Config, &config) == nil && len(config.Inbounds) > 0
	hashOK := parsed && e.Runtime.ConfigHash != "" && healthHash(e.Config) == e.Runtime.ConfigHash
	state, text := "bad", "Файл отличается от применённого"
	if !parsed {
		text = "Конфигурация отсутствует или повреждена"
	} else if e.Runtime.ConfigHash == "" {
		state = "warn"
		text = "Конфигурация ещё не применена"
	} else if hashOK && e.ConfigValid {
		state = "good"
		text = "Соответствует применённой"
	} else if hashOK {
		text = "Проверка Xray не пройдена"
	}
	add("config", "Конфигурация", state, text, "Применённый JSON и успешная проверка Xray", text+". Проверка ядром: "+e.ConfigTested.UTC().Format(time.RFC3339))
	n := e.Runtime.AppliedNetwork
	networkOK := false
	for _, row := range healthRows(e.Addresses) {
		if row["ifname"] != n.Interface {
			continue
		}
		infos, _ := row["addr_info"].([]any)
		for _, info := range infos {
			item, _ := info.(map[string]any)
			if item["local"] == n.Address && strings.HasSuffix(n.CIDR, "/"+fmt.Sprint(item["prefixlen"])) {
				networkOK = true
			}
		}
		flags, _ := row["flags"].([]any)
		up := false
		for _, f := range flags {
			up = up || f == "UP"
		}
		networkOK = networkOK && up
	}
	routeOK := false
	for _, row := range healthRows(e.Routes) {
		if row["dst"] == "default" && row["gateway"] == n.Router && row["dev"] == n.Interface {
			routeOK = true
		}
	}
	if !e.Runtime.Gateway {
		add("network", "Сеть ВМ", "neutral", "Прозрачный шлюз выключен", "Включённый шлюз для проверки сети", "Перехват не запрошен")
	} else {
		summary = "Адрес и маршрут совпадают"
		if !networkOK || !routeOK {
			summary = "Адрес, интерфейс или роутер отличаются"
		}
		add("network", "Сеть ВМ", good(networkOK && routeOK), summary, n.Address+" · "+n.Interface+" → "+n.Router, summary)
	}
	nftOK := canonicalHealthNFT(e.NFT) != "" && canonicalHealthNFT(e.NFT) == canonicalHealthNFT(e.Baseline)
	state = good(nftOK)
	text = "Правила совпадают с эталоном"
	if canonicalHealthNFT(e.NFT) == "" {
		text = "Таблица перехвата недоступна"
	} else if canonicalHealthNFT(e.Baseline) == "" {
		state = "warn"
		text = "Нет эталона для сравнения"
	} else if !nftOK {
		text = "Правила отличаются от эталона"
	}
	if !e.Runtime.Gateway {
		state = "neutral"
		text = "Перехват выключен"
	}
	add("nft", "Перехват nftables", state, text, "Точные цепочки и порядок правил применённого шлюза", text)
	policyOK := false
	for _, row := range healthRows(e.Rules) {
		mark := fmt.Sprint(row["fwmark"])
		mask := fmt.Sprint(row["fwmask"])
		if fmt.Sprint(row["priority"]) == "11000" && fmt.Sprint(row["table"]) == "100" && (mark == "0x1" || mark == "1") && (mask == "<nil>" || mask == "0xffffffff" || mask == "4294967295") {
			policyOK = true
		}
	}
	localOK := false
	for _, row := range healthRows(e.LocalRoutes) {
		if row["type"] == "local" && row["dst"] == "default" && row["dev"] == "lo" {
			localOK = true
		}
	}
	state = good(policyOK && localOK)
	text = "Марка 1 → таблица 100 → lo"
	if !policyOK || !localOK {
		text = "Правило или локальный маршрут отсутствует"
	}
	if !e.Runtime.Gateway {
		state = "neutral"
		text = "Не требуется: перехват выключен"
	}
	add("routing", "Policy routing", state, text, "priority 11000 · fwmark 1 · table 100 · local default dev lo", fmt.Sprintf("Правило: %t; маршрут: %t", policyOK, localOK))
	sysOK := strings.TrimSpace(e.Forward) == "1" && strings.TrimSpace(e.Filter) == "2" && strings.TrimSpace(e.InterfaceFilter) == "2"
	state = good(sysOK)
	text = "Пересылка и rp_filter настроены"
	if !sysOK {
		text = "Сетевые параметры отличаются"
	}
	if !e.Runtime.Gateway {
		state = "neutral"
		text = "Не требуется: перехват выключен"
	}
	add("kernel", "Параметры Linux", state, text, "ip_forward=1 · rp_filter all=2 и интерфейс=2", fmt.Sprintf("ip_forward=%s · all=%s · %s=%s", strings.TrimSpace(e.Forward), strings.TrimSpace(e.Filter), n.Interface, strings.TrimSpace(e.InterfaceFilter)))
	inboundOK := false
	dnsConfig := false
	for _, i := range config.Inbounds {
		if i.Tag == "tproxy-in" && i.Port == 7895 && i.Protocol == "dokodemo-door" && i.Settings.FollowRedirect && i.Settings.Network == "tcp,udp" && i.StreamSettings.Sockopt.Tproxy == "tproxy" {
			inboundOK = true
		}
		if i.Tag == "dns-in" && i.Port == 1053 {
			dnsConfig = true
		}
	}
	portsOK := strings.Contains(e.Listeners, ":7895 ") && strings.Contains(e.Listeners, ":1053 ")
	state = good(inboundOK && portsOK && running)
	text = "Входы 7895 и 1053 слушают"
	if !inboundOK || !portsOK || !running {
		text = "Входы Xray не готовы"
	}
	add("listeners", "Входы Xray", state, text, "TPROXY 7895 и DNS 1053 · TCP/UDP", text)
	dnsOK := dnsConfig && len(config.DNS.Servers) > 0 && e.DNSOK && running
	text = "Отвечает через Xray"
	if !dnsOK {
		text = "DNS не отвечает или не настроен"
	}
	add("dns", "DNS", good(dnsOK), text, "Ответ DNS через 127.0.0.1:1053", text)
	needGeo := map[string]bool{}
	for _, r := range config.Routing.Rules {
		for _, v := range r.Domain {
			if strings.HasPrefix(v, "geosite:") {
				needGeo["geosite.dat"] = true
			}
		}
		for _, v := range r.IP {
			if strings.HasPrefix(v, "geoip:") {
				needGeo["geoip.dat"] = true
			}
		}
	}
	missing := []string{}
	details := []string{}
	for _, name := range []string{"geosite.dat", "geoip.dat"} {
		if needGeo[name] && e.Geo[name] == "" {
			missing = append(missing, name)
		}
		if e.Geo[name] != "" {
			details = append(details, name+": "+e.Geo[name])
		}
	}
	state = "good"
	text = "Базы доступны"
	if len(needGeo) == 0 {
		state = "neutral"
		text = "Не используются правилами"
	} else if len(missing) > 0 {
		state = "bad"
		text = "Отсутствует: " + strings.Join(missing, ", ")
	} else if !e.ConfigValid {
		state = "warn"
		text = "Нужна успешная проверка Xray"
	}
	add("geo", "Базы geosite / geoip", state, text, "Доступные ядру базы для всех geo-категорий конфига", strings.Join(details, "; "))
	enabled := strings.Fields(e.Autostart)
	autoOK := len(enabled) == 2 && enabled[0] == "enabled" && enabled[1] == "enabled"
	text = "Xray и шлюз включены"
	if !autoOK {
		text = "Автозапуск требует проверки"
	}
	state = "good"
	if !autoOK {
		state = "warn"
	}
	add("autostart", "Автозапуск", state, text, "xray и ngpanel-gateway: enabled", strings.TrimSpace(e.Autostart))
	return h
}
func healthDNS() bool {
	c, e := net.DialTimeout("udp", "127.0.0.1:1053", 2*time.Second)
	if e != nil {
		return false
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	packet := []byte{0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
	_, _ = rand.Read(packet[:2])
	if _, e = c.Write(packet); e != nil {
		return false
	}
	reply := make([]byte, 4096)
	n, e := c.Read(reply)
	return e == nil && n >= 12 && reply[0] == packet[0] && reply[1] == packet[1] && reply[2]&128 != 0 && reply[3]&15 == 0 && (reply[6] != 0 || reply[7] != 0)
}

type healthCollector struct {
	signature           string
	valid               bool
	tested              time.Time
	dnsAt               time.Time
	dnsOK               bool
	versionKey, version string
}

func (c *healthCollector) sample() ServiceHealth {
	e := healthEvidence{Runtime: readRuntime(), Geo: map[string]string{}}
	e.Config, _ = os.ReadFile("/etc/ngpanel/config.json")
	e.Baseline, _ = os.ReadFile("/etc/ngpanel/gateway-baseline.json")
	e.NFT, _ = healthCommand("nft", "-j", "list", "table", "inet", "ngpanel")
	e.Addresses, _ = healthCommand("ip", "-j", "-4", "address", "show")
	e.Routes, _ = healthCommand("ip", "-j", "-4", "route", "show")
	e.Rules, _ = healthCommand("ip", "-j", "-4", "rule", "show")
	e.LocalRoutes, _ = healthCommand("ip", "-j", "-4", "route", "show", "table", "100")
	b, _ := healthCommand("systemctl", "show", "xray", "--property=ActiveState,MainPID,ActiveEnterTimestampMonotonic")
	for _, line := range strings.Split(string(b), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "ActiveState":
			e.Active = value
		case "MainPID":
			e.XrayPID, _ = strconv.Atoi(value)
		case "ActiveEnterTimestampMonotonic":
			e.ActiveSince, _ = strconv.ParseUint(value, 10, 64)
		}
	}
	versionKey := healthFileIdentity("/usr/local/bin/xray")
	if versionKey != c.versionKey || c.version == "" {
		b, _ = healthCommand("/usr/local/bin/xray", "version")
		c.version, c.versionKey = string(b), versionKey
	}
	e.Version = c.version
	b, _ = healthCommand("ss", "-H", "-lnt")
	e.Listeners = string(b)
	u, _ := healthCommand("ss", "-H", "-lnu")
	for _, port := range []string{":7895 ", ":1053 "} {
		if !strings.Contains(string(u), port) {
			e.Listeners = ""
		}
	}
	b, _ = healthCommand("systemctl", "is-enabled", "xray", "ngpanel-gateway")
	e.Autostart = string(b)
	b, _ = os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	e.Forward = string(b)
	b, _ = os.ReadFile("/proc/sys/net/ipv4/conf/all/rp_filter")
	e.Filter = string(b)
	iface := e.Runtime.AppliedNetwork.Interface
	if filepath.Base(iface) == iface && iface != "." && iface != ".." {
		b, _ = os.ReadFile(filepath.Join("/proc/sys/net/ipv4/conf", iface, "rp_filter"))
		e.InterfaceFilter = string(b)
	}
	for _, name := range []string{"geosite.dat", "geoip.dat"} {
		if info, err := os.Stat(filepath.Join("/usr/local/share/ngpanel-geodata", name)); err == nil && info.Size() > 0 {
			e.Geo[name] = fmt.Sprintf("%.1f МБ · %s", float64(info.Size())/1048576, info.ModTime().UTC().Format("02.01.2006"))
		}
	}
	account, accountErr := user.Lookup("ngxray")
	accountKey := "missing"
	if accountErr == nil {
		accountKey = account.Uid + ":" + account.Gid
	}
	signature := healthValidationSignature(e.Config, e.Version, accountKey, []string{"/etc/ngpanel/config.json", "/etc/ngpanel", "/usr/local/bin/xray", "/usr/local/share/ngpanel-geodata", "/usr/local/share/ngpanel-geodata/geosite.dat", "/usr/local/share/ngpanel-geodata/geoip.dat"})
	if c.validationDue(signature, time.Now()) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, "/usr/local/bin/xray", "run", "-test", "-c", "/etc/ngpanel/config.json")
		cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET=/usr/local/share/ngpanel-geodata")
		err := accountErr
		c.valid = false
		if err == nil {
			uid, _ := strconv.Atoi(account.Uid)
			gid, _ := strconv.Atoi(account.Gid)
			cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
			c.valid = cmd.Run() == nil
		}
		cancel()
		c.signature = signature
		c.tested = time.Now()
	}
	e.ConfigValid = c.valid
	e.ConfigTested = c.tested
	if time.Since(c.dnsAt) > 30*time.Second {
		c.dnsOK = healthDNS()
		c.dnsAt = time.Now()
	}
	e.DNSOK = c.dnsOK
	h := buildServiceHealth(e)
	h.Host = healthHostSnapshot(e, h)
	return h
}
func collectServiceHealth() error {
	group, e := user.LookupGroup("ngpanel")
	if e != nil {
		return e
	}
	gid, _ := strconv.Atoi(group.Gid)
	collector := healthCollector{}
	for {
		h := collector.sample()
		data, e := json.Marshal(h)
		if e != nil {
			return e
		}
		f, e := os.CreateTemp("/run/ngpanel-health", ".health-")
		if e != nil {
			return e
		}
		name := f.Name()
		e = f.Chown(0, gid)
		if e == nil {
			e = f.Chmod(0640)
		}
		if e == nil {
			_, e = f.Write(data)
		}
		_ = f.Close()
		if e == nil {
			e = os.Rename(name, "/run/ngpanel-health/health.json")
		}
		if e != nil {
			_ = os.Remove(name)
			return e
		}
		time.Sleep(10 * time.Second)
	}
}
func readServiceHealth() ServiceHealth {
	path := "/run/ngpanel-health/health.json"
	if os.Getenv("NG_REVIEW") == "1" {
		path = filepath.Join(stateDir(), "health.json")
	}
	data, e := os.ReadFile(path)
	var h ServiceHealth
	if e != nil || len(data) > 128*1024 || json.Unmarshal(data, &h) != nil || len(h.Checks) == 0 {
		return ServiceHealth{Checks: []ServiceCheck{}, Message: "Проверка компонентов ещё не получена"}
	}
	if time.Since(h.Updated) > 45*time.Second || time.Until(h.Updated) > 5*time.Second {
		h.Message = "Проверка устарела: проверьте ngpanel-health"
		for i := range h.Checks {
			h.Checks[i].State = "unknown"
		}
		return h
	}
	return h
}
