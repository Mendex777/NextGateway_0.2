package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const connectionLimit = 2000

type ConnectionEvent struct {
	Domain     string
	Rule       string
	process    string
	socketID   string
	superseded bool
	TCPState   string
	StartKnown bool
	ClosedAt   *time.Time

	ID                                                                                       string
	Time                                                                                     time.Time
	Source, SourcePort, Device, Destination, Port, Network, Inbound, Outbound, OutboundLabel string
}
type ConnectionSnapshot struct {
	TCPAvailable bool
	Updated      time.Time
	Events       []ConnectionEvent
	Message      string
}

var connectionLine = regexp.MustCompile(`from (?:tcp:|udp:)?(\S+) (accepted|rejected) (tcp|udp):(\S+) \[([^\]]+)\]`)
var connectionGroupTag = regexp.MustCompile(`^auto-vpn-([0-9]+)-([0-9]+)-$`)

func parseConnection(message, id string, at time.Time) (ConnectionEvent, bool) {
	m := connectionLine.FindStringSubmatch(message)
	if m == nil || m[2] != "accepted" {
		return ConnectionEvent{}, false
	}
	source, sourcePort, e := net.SplitHostPort(m[1])
	if e != nil || net.ParseIP(source) == nil {
		return ConnectionEvent{}, false
	}
	destination, port, e := net.SplitHostPort(m[4])
	if e != nil || len(destination) == 0 || len(destination) > 253 {
		return ConnectionEvent{}, false
	}
	for _, value := range []string{sourcePort, port} {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 65535 {
			return ConnectionEvent{}, false
		}
	}
	parts := strings.Fields(m[5])
	if len(parts) != 3 || (parts[1] != "->" && parts[1] != ">>" && parts[1] != "==>") {
		return ConnectionEvent{}, false
	}
	return ConnectionEvent{ID: id, Time: at, StartKnown: true, Source: source, SourcePort: sourcePort, Destination: destination, Port: port, Network: m[3], Inbound: parts[0], Outbound: parts[2]}, true
}

// The privileged collector exports only parsed connection metadata, never raw journal messages.
func collectConnections() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("connection collector requires its system service")
	}
	owner, e := user.Lookup("ngpanel")
	if e != nil {
		return e
	}
	uid, _ := strconv.Atoi(owner.Uid)
	gid, _ := strconv.Atoi(owner.Gid)
	xrayOwner, e := user.Lookup("ngxray")
	if e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	cmd := exec.CommandContext(ctx, "journalctl", "--unit=xray.service", "--follow", "--no-pager", "--output=json", "--since=-15min", "--lines=20000")
	output, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		return e
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	type journalEntry struct {
		message, id, process string
		at                   time.Time
	}
	incoming := make(chan journalEntry, 256)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			var row struct {
				Message string `json:"MESSAGE"`
				Cursor  string `json:"__CURSOR"`
				Time    string `json:"__REALTIME_TIMESTAMP"`
				PID     string `json:"_PID"`
				Boot    string `json:"_BOOT_ID"`
			}
			if json.Unmarshal(scanner.Bytes(), &row) != nil {
				continue
			}
			micros, e := strconv.ParseInt(row.Time, 10, 64)
			if e != nil {
				continue
			}
			_, ok := parseConnection(row.Message, row.Cursor, time.UnixMicro(micros).UTC())
			if len(row.Message) > 4096 || (!ok && !connectionInfo.MatchString(row.Message)) {
				continue
			}
			select {
			case incoming <- journalEntry{message: row.Message, id: row.Cursor, process: row.Boot + "|" + row.PID, at: time.UnixMicro(micros).UTC()}:
			case <-ctx.Done():
				return
			}
		}
	}()
	events := make([]ConnectionEvent, 0, connectionLimit)
	resolver := domainResolver{}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-done:
			return fmt.Errorf("connection journal stream stopped")
		case row := <-incoming:
			resolver.observe(row.message, row.process, row.at)
			if event, ok := parseConnection(row.message, row.id, row.at); ok {
				event.process = row.process
				events = append(events, event)
			}
			resolver.enrich(events, row.at)
		case <-ticker.C:
			now := time.Now().UTC()
			sockets, local, tcpError := readXrayTCP(xrayOwner.Uid)
			if tcpError == nil {
				events = updateTCPStates(events, sockets, local, now)
			}
			events = retainConnectionEvents(events, now)
			resolver.prune(time.Now())
			snapshot := ConnectionSnapshot{Updated: now, Events: events, TCPAvailable: tcpError == nil}
			if tcpError != nil {
				snapshot.Message = "Не удалось проверить TCP-сокеты Xray; текущее состояние TCP недоступно"
			}
			raw, e := json.Marshal(snapshot)
			if e != nil {
				return e
			}
			f, e := os.CreateTemp("/run/ngpanel-connections", ".events-*")
			if e != nil {
				return e
			}
			name := f.Name()
			if e = f.Chown(uid, gid); e == nil {
				_, e = f.Write(raw)
			}
			closeErr := f.Close()
			if e == nil {
				e = closeErr
			}
			if e == nil {
				e = os.Rename(name, "/run/ngpanel-connections/events.json")
			}
			if e != nil {
				_ = os.Remove(name)
				return e
			}
		}
	}
}

func connectionSnapshotPath() string {
	if os.Getenv("NG_REVIEW") == "1" {
		return filepath.Join(stateDir(), "connections.json")
	}
	return "/run/ngpanel-connections/events.json"
}

// Internal collectors need the recorded tuples, without UI labels or a second JSON decode.
func rawConnectionSnapshot() ([]byte, error) {
	f, err := os.Open(connectionSnapshotPath())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 4*1024*1024 {
		return nil, fmt.Errorf("connection snapshot too large")
	}
	return raw, nil
}
func connectionSnapshot() ConnectionSnapshot { return connectionSnapshotForView(false) }
func connectionSnapshotForView(activeOnly bool) ConnectionSnapshot {
	result := ConnectionSnapshot{Events: []ConnectionEvent{}}
	path := connectionSnapshotPath()
	f, e := os.Open(path)
	if e != nil {
		result.Message = "Сборщик журнала соединений ещё не запущен"
		return result
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || info.Size() > 4*1024*1024 {
		result.Message = "Журнал соединений недоступен"
		return result
	}
	if json.NewDecoder(f).Decode(&result) != nil {
		result.Events = []ConnectionEvent{}
		result.Message = "Не удалось прочитать журнал соединений"
		return result
	}
	if len(result.Events) > connectionLimit {
		result.Events = result.Events[len(result.Events)-connectionLimit:]
	}
	if activeOnly {
		active := make([]ConnectionEvent, 0)
		for _, event := range result.Events {
			if event.Network == "tcp" && event.Inbound == "tproxy-in" && event.TCPState == "active" {
				active = append(active, event)
			}
		}
		result.Events = active
	}
	names := map[string]string{}
	for _, d := range devices() {
		names[d.IP] = d.Name
	}
	nodeNames := map[string]string{}
	rows, e := db.Query("SELECT id,name FROM nodes")
	if e == nil {
		for rows.Next() {
			var id, name string
			if rows.Scan(&id, &name) == nil {
				nodeNames[id] = name
			}
		}
		rows.Close()
	}
	groupNames := map[string]string{}
	for _, g := range balanceGroups() {
		groupNames[g.ID] = g.Name
	}
	for i := range result.Events {
		event := &result.Events[i]
		event.Device = names[event.Source]
		switch event.Outbound {
		case "direct":
			event.OutboundLabel = "Direct — провайдер"
		case "block":
			event.OutboundLabel = "Блокировка"
		case "proxy":
			event.OutboundLabel = "Выбранный VPN"
		case "dns-out":
			event.OutboundLabel = "DNS"
		default:
			if match := connectionGroupTag.FindStringSubmatch(event.Outbound); match != nil {
				event.OutboundLabel = groupNames[match[1]]
				if name := nodeNames[match[2]]; name != "" {
					if event.OutboundLabel != "" {
						event.OutboundLabel += " → "
					}
					event.OutboundLabel += name
				}
			} else if strings.HasPrefix(event.Outbound, "node-") {
				event.OutboundLabel = nodeNames[strings.TrimPrefix(event.Outbound, "node-")]
			}
		}
		if event.OutboundLabel == "" {
			event.OutboundLabel = event.Outbound
			if event.OutboundLabel == "" {
				event.OutboundLabel = "Не определён"
			}
		}
	}
	if time.Since(result.Updated) > 15*time.Second {
		result.Message = "Журнал давно не обновлялся: показаны последние полученные записи"
	}
	if time.Since(result.Updated) > 3*time.Second {
		result.TCPAvailable = false
		result.Message = "Состояние TCP устарело: активные соединения временно недоступны; история сохранена"
	}
	return result
}
