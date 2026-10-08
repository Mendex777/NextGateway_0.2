package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ProbeResult struct {
	State, Message, Checked string
	Mode                    string
	RunID, ExitIP, Country  string
	TCPMS, HTTPSMS          int64
	UDP                     bool
}

var probeLock sync.Mutex

func startProbe(id string, modes ...string) error {
	mode, err := probeMode(modes)
	if err != nil {
		return err
	}
	if nodeDisabled(id) {
		return fmt.Errorf("Подписка отключена")
	}
	var raw string
	if e := db.QueryRow("SELECT uri FROM nodes WHERE id=?", id).Scan(&raw); e != nil {
		return fmt.Errorf("Подключение не найдено")
	}
	if !probeLock.TryLock() {
		return fmt.Errorf("Проверка подключения уже выполняется")
	}
	runID := probeRunID(modes)
	saveProbe(id, ProbeResult{State: "running", Mode: mode, RunID: runID, Message: "Проверяется…"})
	go func() {
		defer probeLock.Unlock()
		result := probeNodeMode(context.Background(), raw, mode)
		result.RunID = runID
		saveProbe(id, result)
	}()
	return nil
}
func probeRunID(modes []string) string {
	if len(modes) > 1 && len(modes[1]) <= 128 {
		return modes[1]
	}
	return ""
}
func traceEgress(body []byte) (string, string) {
	var ip, country string
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		if key == "ip" && net.ParseIP(value) != nil {
			ip = value
		}
		if key == "loc" && len(value) == 2 && value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z' {
			country = value
		}
	}
	return ip, country
}
func saveProbe(id string, result ProbeResult) {
	b, _ := json.Marshal(result)
	saveSetting("node_probe:"+id, string(b))
}
func probeNode(raw string) ProbeResult { return probeNodeContext(context.Background(), raw) }
func probeNodeContext(parent context.Context, raw string) ProbeResult {
	return probeNodeMode(parent, raw, "real")
}
func probeMode(modes []string) (string, error) {
	mode := "real"
	if len(modes) > 0 && modes[0] != "" {
		mode = modes[0]
	}
	switch mode {
	case "tcp", "http", "real":
		return mode, nil
	}
	return "", fmt.Errorf("Неизвестный режим проверки")
}
func probeNodeMode(parent context.Context, raw, mode string) ProbeResult {
	var result ProbeResult
	runProbeJobs(parent, []probeJob{{ID: "single", Raw: raw}}, mode, func(_ probeJob, r ProbeResult) { result = r })
	if result.State == "" {
		result = ProbeResult{State: "error", Mode: mode, Message: "Проверка отменена"}
	}
	return result
}
func socksConnect(ctx context.Context, proxy, target string) (net.Conn, error) {
	c, e := (&net.Dialer{}).DialContext(ctx, "tcp", proxy)
	if e != nil {
		return nil, e
	}
	failed := true
	defer func() {
		if failed {
			c.Close()
		}
	}()
	if deadline, ok := ctx.Deadline(); ok {
		c.SetDeadline(deadline)
	} else {
		c.SetDeadline(time.Now().Add(10 * time.Second))
	}
	if _, e = c.Write([]byte{5, 1, 0}); e != nil {
		return nil, e
	}
	reply := make([]byte, 2)
	if _, e = io.ReadFull(c, reply); e != nil || reply[0] != 5 || reply[1] != 0 {
		return nil, fmt.Errorf("SOCKS negotiation failed")
	}
	host, port, e := net.SplitHostPort(target)
	if e != nil || len(host) > 255 {
		return nil, fmt.Errorf("Invalid target")
	}
	number, e := strconv.Atoi(port)
	if e != nil {
		return nil, e
	}
	payload := append([]byte{5, 1, 0, 3, byte(len(host))}, []byte(host)...)
	payload = append(payload, byte(number>>8), byte(number))
	if _, e = c.Write(payload); e != nil {
		return nil, e
	}
	header := make([]byte, 4)
	if _, e = io.ReadFull(c, header); e != nil || header[1] != 0 {
		return nil, fmt.Errorf("SOCKS connection rejected")
	}
	size := 0
	switch header[3] {
	case 1:
		size = 4
	case 4:
		size = 16
	case 3:
		one := make([]byte, 1)
		if _, e = io.ReadFull(c, one); e != nil {
			return nil, e
		}
		size = int(one[0])
	default:
		return nil, fmt.Errorf("Invalid SOCKS reply")
	}
	if _, e = io.CopyN(io.Discard, c, int64(size+2)); e != nil {
		return nil, e
	}
	c.SetDeadline(time.Time{})
	failed = false
	return c, nil
}
