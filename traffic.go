package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type TrafficValue struct {
	UpBytes, DownBytes uint64
	UpBPS, DownBPS     float64
}
type TrafficSnapshot struct {
	History                              []TrafficHistoryPoint `json:"History,omitempty"`
	Updated, Started                     time.Time
	DevicesAvailable, OutboundsAvailable bool
	Message                              string
	Devices, Outbounds                   map[string]TrafficValue
	Routes                               []TrafficRoute
	RoutesAvailable                      bool
	Probes                               []MapProbe
}
type trafficRaw struct{ Up, Down uint64 }
type trafficRecord struct {
	Total, Last trafficRaw
	Generation  string
	At          time.Time
}

// Source generations prevent double counting across collector restarts, while
// preserving totals across core restarts and nft table replacement.
func advanceTraffic(old trafficRecord, raw trafficRaw, generation string, now time.Time) (trafficRecord, TrafficValue) {
	delta := raw
	same := old.Generation == generation && generation != ""
	if same {
		if raw.Up >= old.Last.Up {
			delta.Up = raw.Up - old.Last.Up
		}
		if raw.Down >= old.Last.Down {
			delta.Down = raw.Down - old.Last.Down
		}
	}
	value := TrafficValue{UpBytes: old.Total.Up + delta.Up, DownBytes: old.Total.Down + delta.Down}
	seconds := now.Sub(old.At).Seconds()
	if same && !old.At.IsZero() && seconds > 0 && seconds <= 10 && raw.Up >= old.Last.Up && raw.Down >= old.Last.Down {
		value.UpBPS = float64(delta.Up) / seconds
		value.DownBPS = float64(delta.Down) / seconds
	}
	return trafficRecord{Total: trafficRaw{value.UpBytes, value.DownBytes}, Last: raw, Generation: generation, At: now}, value
}
func trafficNFT(n GatewayNetwork, uid string, exits ...trafficExit) (string, string, error) {
	if err := n.validate(); err != nil {
		return "", "", err
	}
	if _, err := strconv.ParseUint(uid, 10, 32); err != nil {
		return "", "", err
	}
	encoded, _ := json.Marshal(exits)
	fingerprint := fmt.Sprintf("ngpanel-traffic-v3-%x", sha256.Sum256([]byte(n.Interface+"|"+n.Address+"|"+n.CIDR+"|"+uid+"|"+string(encoded))))
	text := fmt.Sprintf(`table inet ngpanel_traffic {
 comment %q
 set upload { type ipv4_addr; flags dynamic; size 4096; counter; }
 set download { type ipv4_addr; flags dynamic; size 4096; counter; }
 set flow_upload { type ipv4_addr . inet_service . ipv4_addr . inet_service . inet_proto; flags dynamic,timeout; timeout 90s; gc-interval 15s; size 32768; counter; }
 set flow_download { type ipv4_addr . inet_service . ipv4_addr . inet_service . inet_proto; flags dynamic,timeout; timeout 90s; gc-interval 15s; size 32768; counter; }
 chain client_upload {
  type filter hook prerouting priority -145; policy accept;
  iifname %q ip saddr %s meta mark 1 meta l4proto { tcp, udp } update @upload { ip saddr counter } update @flow_upload { ip saddr . th sport . ip daddr . th dport . meta l4proto counter }
 }
 chain client_download {
  type filter hook output priority 0; policy accept;
  oifname %q meta skuid %s ip daddr %s ip saddr != %s meta l4proto { tcp, udp } update @download { ip daddr counter } update @flow_download { ip daddr . th dport . ip saddr . th sport . meta l4proto counter }
 }
}
`, fingerprint, n.Interface, n.CIDR, n.Interface, uid, n.CIDR, n.Address)
	var definitions, upload, download strings.Builder
	for _, exit := range exits {
		fmt.Fprintf(&definitions, " counter u_%x { comment %q; }\n counter d_%x { comment %q; }\n", exit.Mark, "upload:"+exit.Tag, exit.Mark, "download:"+exit.Tag)
		fmt.Fprintf(&upload, "  meta skuid %s meta mark %d ct direction original meta l4proto { tcp, udp } ct mark set meta mark counter name u_%x\n", uid, exit.Mark, exit.Mark)
		fmt.Fprintf(&download, "  ct direction reply ct mark %d meta l4proto { tcp, udp } counter name d_%x\n", exit.Mark, exit.Mark)
	}
	extra := definitions.String() + " chain exit_upload { type filter hook output priority 0; policy accept;\n" + upload.String() + " }\n chain exit_download { type filter hook prerouting priority -140; policy accept;\n" + download.String() + " }\n"
	text = strings.TrimSuffix(strings.TrimSpace(text), "}") + extra + "}\n"
	return text, fingerprint, nil
}
func trafficCommand(ctx context.Context, input string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	return cmd.Output()
}
func parseTrafficNFT(raw []byte) (map[string]trafficRaw, string, string, error) {
	var doc struct {
		Nftables []json.RawMessage `json:"nftables"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, "", "", err
	}
	counters := map[string]trafficRaw{}
	generation, comment := "", ""
	for _, row := range doc.Nftables {
		var item struct {
			Counter *struct {
				Name, Comment string
				Bytes         uint64
			} `json:"counter"`
			Table *struct {
				Handle  uint64
				Comment string
			} `json:"table"`
			Set *struct {
				Name string
				Elem []struct {
					Elem struct {
						Val     json.RawMessage
						Counter struct{ Bytes uint64 }
					}
				}
			} `json:"set"`
		}
		if err := json.Unmarshal(row, &item); err != nil {
			return nil, "", "", err
		}
		if item.Counter != nil {
			direction, tag, ok := strings.Cut(item.Counter.Comment, ":")
			if ok && tag != "" && (direction == "upload" || direction == "download") {
				key := "exit:" + tag
				v := counters[key]
				if direction == "upload" {
					v.Up = item.Counter.Bytes
				} else {
					v.Down = item.Counter.Bytes
				}
				counters[key] = v
			}
		}
		if item.Table != nil {
			generation = strconv.FormatUint(item.Table.Handle, 10)
			comment = item.Table.Comment
		}
		if item.Set == nil || (item.Set.Name != "upload" && item.Set.Name != "download" && item.Set.Name != "flow_upload" && item.Set.Name != "flow_download") {
			continue
		}
		for _, entry := range item.Set.Elem {
			if strings.HasPrefix(item.Set.Name, "flow_") {
				key, err := trafficTupleValue(entry.Elem.Val)
				if err != nil {
					return nil, "", "", err
				}
				key = "flow:" + key
				v := counters[key]
				if item.Set.Name == "flow_upload" {
					v.Up = entry.Elem.Counter.Bytes
				} else {
					v.Down = entry.Elem.Counter.Bytes
				}
				counters[key] = v
				continue
			}
			var address string
			if err := json.Unmarshal(entry.Elem.Val, &address); err != nil {
				return nil, "", "", err
			}
			ip := net.ParseIP(address)
			if ip == nil || ip.To4() == nil {
				return nil, "", "", fmt.Errorf("invalid accounting address")
			}
			key := ip.String()
			v := counters[key]
			if item.Set.Name == "upload" {
				v.Up = entry.Elem.Counter.Bytes
			} else {
				v.Down = entry.Elem.Counter.Bytes
			}
			counters[key] = v
		}
	}
	if generation == "" {
		return nil, "", "", fmt.Errorf("missing accounting table")
	}
	return counters, generation, comment, nil
}
func readTrafficNFT(ctx context.Context, n GatewayNetwork, uid, boot string, exits ...trafficExit) (map[string]trafficRaw, string, error) {
	text, want, err := trafficNFT(n, uid, exits...)
	if err != nil {
		return nil, "", err
	}
	raw, listErr := trafficCommand(ctx, "", "nft", "-j", "-a", "list", "table", "inet", "ngpanel_traffic")
	counters, generation, comment, parseErr := parseTrafficNFT(raw)
	if listErr == nil && parseErr != nil {
		return nil, "", parseErr
	}
	if listErr == nil && parseErr == nil && comment == want {
		return counters, boot + "/nft/" + generation, nil
	}
	if listErr == nil {
		text = "delete table inet ngpanel_traffic\n" + text
	}
	if _, err = trafficCommand(ctx, text, "nft", "-c", "-f", "-"); err != nil {
		return nil, "", err
	}
	if _, err = trafficCommand(ctx, text, "nft", "-f", "-"); err != nil {
		return nil, "", err
	}
	raw, err = trafficCommand(ctx, "", "nft", "-j", "-a", "list", "table", "inet", "ngpanel_traffic")
	if err != nil {
		return nil, "", err
	}
	counters, generation, _, err = parseTrafficNFT(raw)
	return counters, boot + "/nft/" + generation, err
}

// Each actual dialling outbound receives its own SO_MARK. Low bits retain
// the gateway bypass mark; upper bits identify the exit without changing routes.
type trafficExit struct {
	Tag  string
	Mark uint32
}

func assignTrafficMarks(config map[string]any) {
	out, _ := config["outbounds"].([]any)
	entries := make([]map[string]any, 0, len(out))
	for _, raw := range out {
		o, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		protocol, _ := o["protocol"].(string)
		if protocol == "freedom" || protocol == "vless" || protocol == "hysteria" {
			entries = append(entries, o)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return fmt.Sprint(entries[i]["tag"]) < fmt.Sprint(entries[j]["tag"]) })
	used := map[uint32]bool{}
	for _, o := range entries {
		h := fnv.New32a()
		h.Write([]byte(fmt.Sprint(o["tag"])))
		id := h.Sum32()%32767 + 1
		for used[id] {
			id = id%32767 + 1
		}
		used[id] = true
		stream, _ := o["streamSettings"].(map[string]any)
		if stream == nil {
			stream = map[string]any{}
			o["streamSettings"] = stream
		}
		sock, _ := stream["sockopt"].(map[string]any)
		if sock == nil {
			sock = map[string]any{}
			stream["sockopt"] = sock
		}
		sock["mark"] = int(id<<16 | 666)
	}
}
func parseTrafficExits(raw []byte) ([]trafficExit, error) {
	var config struct {
		Outbounds []struct {
			Tag, Protocol  string
			StreamSettings struct{ Sockopt struct{ Mark uint32 } }
		}
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	exits := []trafficExit{}
	used := map[uint32]bool{}
	for _, o := range config.Outbounds {
		if o.Protocol != "freedom" && o.Protocol != "vless" && o.Protocol != "hysteria" {
			continue
		}
		mark := o.StreamSettings.Sockopt.Mark
		if o.Tag == "" || mark&65535 != 666 || mark>>16 == 0 || mark>>31 != 0 || used[mark] {
			return nil, fmt.Errorf("outbound accounting marks unavailable")
		}
		used[mark] = true
		exits = append(exits, trafficExit{o.Tag, mark})
	}
	if len(exits) == 0 {
		return nil, fmt.Errorf("no accounting exits")
	}
	sort.Slice(exits, func(i, j int) bool { return exits[i].Tag < exits[j].Tag })
	return exits, nil
}
func collectTraffic() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("traffic collector requires its system service")
	}
	owner, err := user.Lookup("ngpanel")
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(owner.Gid)
	if err != nil {
		return err
	}
	core, err := user.Lookup("ngxray")
	if err != nil {
		return err
	}
	bootRaw, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	boot := strings.TrimSpace(string(bootRaw))
	const store = "/var/lib/ngpanel-traffic/history.db"
	history, err := sql.Open("sqlite3", store+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return err
	}
	defer history.Close()
	history.SetMaxOpenConns(1)
	if _, err = history.Exec(`CREATE TABLE IF NOT EXISTS counters (key TEXT PRIMARY KEY,total_up INTEGER,total_down INTEGER,last_up INTEGER,last_down INTEGER,generation TEXT); CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY,value TEXT)`); err != nil {
		return err
	}
	started := time.Now().UTC()
	var since string
	if history.QueryRow("SELECT value FROM meta WHERE key='started'").Scan(&since) == nil {
		if t, e := time.Parse(time.RFC3339Nano, since); e == nil {
			started = t
		}
	} else {
		if _, err = history.Exec("INSERT INTO meta(key,value) VALUES('started',?)", started.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	records := map[string]trafficRecord{}
	rows, err := history.Query("SELECT key,total_up,total_down,last_up,last_down,generation FROM counters")
	if err != nil {
		return err
	}
	for rows.Next() {
		var key string
		var r trafficRecord
		if err = rows.Scan(&key, &r.Total.Up, &r.Total.Down, &r.Last.Up, &r.Last.Down, &r.Generation); err != nil {
			rows.Close()
			return err
		}
		records[key] = r
	}
	rows.Close()
	var chartRaw string
	_ = history.QueryRow("SELECT value FROM meta WHERE key='chart-history'").Scan(&chartRaw)
	chartHistory := restoreTrafficHistory(chartRaw, time.Now())
	persist := func() error {
		tx, e := history.Begin()
		if e != nil {
			return e
		}
		defer tx.Rollback()
		stmt, e := tx.Prepare("INSERT INTO counters VALUES(?,?,?,?,?,?) ON CONFLICT(key) DO UPDATE SET total_up=excluded.total_up,total_down=excluded.total_down,last_up=excluded.last_up,last_down=excluded.last_down,generation=excluded.generation")
		if e != nil {
			return e
		}
		defer stmt.Close()
		for k, r := range records {
			if _, e = stmt.Exec(k, r.Total.Up, r.Total.Down, r.Last.Up, r.Last.Down, r.Generation); e != nil {
				return e
			}
		}
		chartJSON, _ := json.Marshal(chartHistory)
		if _, e = tx.Exec("INSERT INTO meta(key,value) VALUES('chart-history',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(chartJSON)); e != nil {
			return e
		}
		return tx.Commit()
	}
	defer persist()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	step := 0
	routes := trafficRouteTracker{}
	for {
		snapshot := TrafficSnapshot{Updated: time.Now().UTC(), Started: started, Devices: map[string]TrafficValue{}, Outbounds: map[string]TrafficValue{}}
		// Historical totals remain visible when a device/exit is idle or removed.
		for key, r := range records {
			value := TrafficValue{UpBytes: r.Total.Up, DownBytes: r.Total.Down}
			if strings.HasPrefix(key, "device:") {
				snapshot.Devices[strings.TrimPrefix(key, "device:")] = value
			} else if strings.HasPrefix(key, "exit:") {
				snapshot.Outbounds[strings.TrimPrefix(key, "exit:")] = value
			}
		}
		runtime := readRuntime()
		if runtime.Gateway {
			call, cancel := context.WithTimeout(ctx, 2*time.Second)
			configRaw, e := os.ReadFile("/etc/ngpanel/config.json")
			var exits []trafficExit
			if e == nil {
				exits, e = parseTrafficExits(configRaw)
			}
			var raw map[string]trafficRaw
			var generation string
			if e == nil {
				raw, generation, e = readTrafficNFT(call, runtime.AppliedNetwork, core.Uid, boot, exits...)
			}
			cancel()
			if e == nil {
				snapshot.DevicesAvailable = true
				snapshot.OutboundsAvailable = true
				snapshot.Routes = routes.update(raw, generation, readTrafficConnections(snapshot.Updated), snapshot.Updated, exits)
				snapshot.RoutesAvailable = true
				for ip, count := range raw {
					if strings.HasPrefix(ip, "flow:") {
						continue
					}
					key := "device:" + ip
					if strings.HasPrefix(ip, "exit:") {
						key = ip
					}
					r, v := advanceTraffic(records[key], count, generation, snapshot.Updated)
					records[key] = r
					if strings.HasPrefix(key, "exit:") {
						snapshot.Outbounds[strings.TrimPrefix(key, "exit:")] = v
					} else {
						snapshot.Devices[ip] = v
					}
				}
			} else {
				snapshot.Message = "Счётчики Linux недоступны: проверьте применение конфигурации и службу учёта"
			}
		} else {
			snapshot.Message = "Перехват трафика не включён"
		}
		chartHistory = appendTrafficHistory(chartHistory, snapshot)
		snapshot.History = chartHistory
		data, e := json.Marshal(snapshot)
		if e != nil {
			return e
		}
		file, e := os.CreateTemp("/run/ngpanel-traffic", ".traffic-*")
		if e != nil {
			return e
		}
		name := file.Name()
		if e = file.Chown(0, gid); e == nil {
			e = file.Chmod(0640)
		}
		if e == nil {
			_, e = file.Write(data)
		}
		closeErr := file.Close()
		if e == nil {
			e = closeErr
		}
		if e == nil {
			e = os.Rename(name, "/run/ngpanel-traffic/traffic.json")
		}
		if e != nil {
			os.Remove(name)
			return e
		}
		step++
		if step%10 == 0 {
			if e = persist(); e != nil {
				return e
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
func trafficSnapshot() TrafficSnapshot {
	result := TrafficSnapshot{Devices: map[string]TrafficValue{}, Outbounds: map[string]TrafficValue{}}
	path := "/run/ngpanel-traffic/traffic.json"
	if os.Getenv("NG_REVIEW") == "1" {
		path = filepath.Join(stateDir(), "traffic.json")
	}
	file, err := os.Open(path)
	if err != nil {
		result.Message = "Сборщик трафика ещё не запущен"
		return result
	}
	defer file.Close()
	if json.NewDecoder(io.LimitReader(file, 2*1024*1024)).Decode(&result) != nil {
		return TrafficSnapshot{Message: "Снимок трафика недоступен", Devices: map[string]TrafficValue{}, Outbounds: map[string]TrafficValue{}}
	}
	if time.Since(result.Updated) > 5*time.Second || time.Until(result.Updated) > 5*time.Second {
		result.DevicesAvailable = false
		result.OutboundsAvailable = false
		result.RoutesAvailable = false
		result.Routes = nil
		result.Message = "Снимок трафика устарел"
		for k, v := range result.Devices {
			v.UpBPS = 0
			v.DownBPS = 0
			result.Devices[k] = v
		}
		for k, v := range result.Outbounds {
			v.UpBPS = 0
			v.DownBPS = 0
			result.Outbounds[k] = v
		}
	}
	if os.Getenv("NG_REVIEW") != "1" {
		result.Probes = mapProbeSnapshot()
	}
	return result
}
func enableTrafficStats(config map[string]any) {
	assignTrafficMarks(config)
	config["stats"] = map[string]any{}
	config["policy"] = map[string]any{"system": map[string]any{"statsInboundUplink": true, "statsInboundDownlink": true, "statsOutboundUplink": true, "statsOutboundDownlink": true}}
	if api, ok := config["api"].(map[string]any); ok {
		api["services"] = []string{"RoutingService", "ObservatoryService", "StatsService"}
	} else {
		config["api"] = map[string]any{"tag": "balance-api", "listen": "127.0.0.1:10085", "services": []string{"StatsService"}}
	}
}
func ensureTrafficCollector() {
	for attempt := 0; attempt < 10; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		raw, err := exec.CommandContext(ctx, "systemctl", "show", "ngpanel-traffic.service", "--property=LoadState", "--value").Output()
		cancel()
		if err == nil {
			state := strings.TrimSpace(string(raw))
			if state != "not-found" {
				return
			} // Respect installed, disabled and masked units.
			if enqueue("traffic", nil) == nil {
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
}
