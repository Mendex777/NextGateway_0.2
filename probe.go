package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type ProbeResult struct {
	State, Message, Checked string
	Mode                    string
	TCPMS, HTTPSMS          int64
	UDP                     bool
}

var probeLock sync.Mutex

func startProbe(id string, modes ...string) error {
	mode, err := probeMode(modes)
	if err != nil {
		return err
	}
	var raw string
	if e := db.QueryRow("SELECT uri FROM nodes WHERE id=?", id).Scan(&raw); e != nil {
		return fmt.Errorf("Подключение не найдено")
	}
	if !probeLock.TryLock() {
		return fmt.Errorf("Проверка подключения уже выполняется")
	}
	saveProbe(id, ProbeResult{State: "running", Message: "Проверяется…"})
	go func() {
		defer probeLock.Unlock()
		result := probeNodeMode(context.Background(), raw, mode)
		saveProbe(id, result)
	}()
	return nil
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
	result := ProbeResult{State: "error", Mode: mode, Checked: time.Now().UTC().Format(time.RFC3339)}
	outbound, e := nodeOutbound(raw)
	if e != nil {
		result.Message = e.Error()
		return result
	}
	result.UDP = outbound["protocol"] == "hysteria"
	prefix := ""
	start := time.Now()
	if !result.UDP {
		host := outbound["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)
		connection, err := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(parent, "tcp4", net.JoinHostPort(host["address"].(string), fmt.Sprint(host["port"])))
		if err != nil {
			result.Message = "Сервер недоступен по TCP"
			return result
		}
		connection.Close()
		result.TCPMS = time.Since(start).Milliseconds()
		prefix = "TCP доступен; "
		if mode == "tcp" {
			result.State = "ok"
			result.HTTPSMS = result.TCPMS
			result.Message = "TCP-соединение с сервером установлено"
			return result
		}
	}
	if result.UDP && mode == "tcp" {
		result.State = "unsupported"
		result.Message = "Hysteria использует UDP/QUIC: выберите HTTP или реальную задержку"
		return result
	}
	outbound["streamSettings"].(map[string]any)["sockopt"] = map[string]any{"domainStrategy": "UseIPv4"}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		result.Message = "Не удалось выделить порт проверки"
		return result
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	dir, e := os.MkdirTemp(filepath.Join(stateDir(), "db"), ".probe-")
	if e != nil {
		result.Message = "Не удалось подготовить проверку"
		return result
	}
	defer os.RemoveAll(dir)
	config := map[string]any{"log": map[string]any{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "noauth"}}}, "outbounds": []any{outbound}}
	b, _ := json.Marshal(config)
	path := filepath.Join(dir, "config.json")
	if e = os.WriteFile(path, b, 0o600); e != nil {
		result.Message = "Ошибка подготовки проверки"
		return result
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	test := exec.CommandContext(ctx, "/usr/local/bin/xray", "run", "-test", "-c", path)
	if test.Run() != nil {
		result.Message = prefix + "конфигурация отклонена Xray"
		return result
	}
	process := exec.CommandContext(ctx, "/usr/local/bin/xray", "run", "-c", path)
	if process.Start() != nil {
		result.Message = prefix + "не удалось запустить тестовый Xray"
		return result
	}
	defer func() { process.Process.Kill(); process.Wait() }()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	ready := false
	for i := 0; i < 30; i++ {
		c, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		result.Message = prefix + "тестовый Xray не запустился"
		return result
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, target string) (net.Conn, error) {
		return socksConnect(ctx, address, target)
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	start = time.Now()
	request, _ := http.NewRequestWithContext(ctx, "GET", "https://www.cloudflare.com/cdn-cgi/trace", nil)
	response, e := client.Do(request)
	if e != nil {
		result.Message = prefix + "HTTPS через VPN не прошёл"
		return result
	}
	defer response.Body.Close()
	body, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	if e != nil || response.StatusCode != 200 || len(body) == 0 || len(body) > 65536 {
		result.Message = prefix + "тестовый HTTPS-сервер не вернул корректный ответ"
		return result
	}
	result.HTTPSMS = time.Since(start).Milliseconds()
	if mode == "http" {
		response.Body.Close()
		start = time.Now()
		warmRequest, _ := http.NewRequestWithContext(ctx, "GET", "https://www.cloudflare.com/cdn-cgi/trace", nil)
		warmResponse, err := client.Do(warmRequest)
		if err != nil {
			result.Message = "Повторный HTTP-запрос через VPN не прошёл"
			return result
		}
		_, err = io.Copy(io.Discard, io.LimitReader(warmResponse.Body, 65537))
		warmResponse.Body.Close()
		if err != nil || warmResponse.StatusCode != 200 {
			result.Message = "HTTP-сервер не вернул корректный ответ"
			return result
		}
		result.HTTPSMS = time.Since(start).Milliseconds()
	}
	result.State = "ok"
	result.Message = "TCP и HTTPS через VPN работают"
	if result.UDP {
		result.Message = "HTTPS через Hysteria 2 (UDP/QUIC) работает"
	}
	if mode == "http" {
		result.Message += "; задержка повторного запроса по готовому соединению"
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
