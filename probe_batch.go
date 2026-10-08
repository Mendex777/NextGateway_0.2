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

const probeTCPWorkers = 8
const probeHTTPChunk = 16
const probeTestURL = "https://www.google.com/generate_204"
const probeTraceURL = "https://www.cloudflare.com/cdn-cgi/trace"

type probeJob struct {
	ID, Raw  string
	Outbound map[string]any
}

func probeError(mode, message string) ProbeResult {
	return ProbeResult{State: "error", Mode: mode, Message: message, Checked: time.Now().UTC().Format(time.RFC3339Nano)}
}

// TCP and HTTP have separate lanes: a slow UDP proxy never holds up TCP dials.
// Only one HTTP chunk runs at a time, keeping the number of temp cores bounded.
func runProbeJobs(ctx context.Context, jobs []probeJob, mode string, complete func(probeJob, ProbeResult)) {
	var tcp, httpJobs []probeJob
	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}
		out, err := nodeOutbound(job.Raw)
		if err != nil {
			complete(job, probeError(mode, err.Error()))
			continue
		}
		job.Outbound = out
		if mode == "tcp" && out["protocol"] != "hysteria" {
			tcp = append(tcp, job)
		} else {
			httpJobs = append(httpJobs, job)
		}
	}
	var lanes sync.WaitGroup
	lanes.Add(2)
	go func() {
		defer lanes.Done()
		parallelProbes(ctx, tcp, probeTCPWorkers, func(job probeJob) { complete(job, probeTCP(ctx, job.Outbound)) })
	}()
	go func() {
		defer lanes.Done()
		effective := mode
		if effective == "tcp" {
			effective = "http"
		}
		for at := 0; at < len(httpJobs) && ctx.Err() == nil; at += probeHTTPChunk {
			end := min(at+probeHTTPChunk, len(httpJobs))
			probeHTTPBatch(ctx, httpJobs[at:end], effective, complete)
		}
	}()
	lanes.Wait()
}

func parallelProbes(ctx context.Context, jobs []probeJob, workers int, work func(probeJob)) {
	queue := make(chan probeJob)
	var wg sync.WaitGroup
	for i := 0; i < min(workers, len(jobs)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range queue {
				if ctx.Err() == nil {
					work(job)
				}
			}
		}()
	}
	for _, job := range jobs {
		select {
		case queue <- job:
		case <-ctx.Done():
			close(queue)
			wg.Wait()
			return
		}
	}
	close(queue)
	wg.Wait()
}

func probeTCP(ctx context.Context, out map[string]any) ProbeResult {
	r := probeError("tcp", "Сервер недоступен по TCP")
	host := out["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)
	start := time.Now()
	c, err := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort(host["address"].(string), fmt.Sprint(host["port"])))
	if err == nil {
		c.Close()
		r.State, r.Message = "ok", "TCP-соединение с сервером установлено"
		r.TCPMS = max(time.Since(start).Milliseconds(), 1)
		r.HTTPSMS = r.TCPMS
	}
	return r
}

func sharedProbeConfig(jobs []probeJob, ports []int) map[string]any {
	inbounds, outbounds, rules := []any{}, []any{}, []any{}
	for i, job := range jobs {
		inTag, outTag := fmt.Sprintf("probe-in-%d", i), fmt.Sprintf("probe-out-%d", i)
		job.Outbound["tag"] = outTag
		job.Outbound["streamSettings"].(map[string]any)["sockopt"] = map[string]any{"domainStrategy": "UseIPv4"}
		inbounds = append(inbounds, map[string]any{"tag": inTag, "listen": "127.0.0.1", "port": ports[i], "protocol": "socks", "settings": map[string]any{"auth": "noauth"}})
		outbounds = append(outbounds, job.Outbound)
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{inTag}, "outboundTag": outTag})
	}
	return map[string]any{"log": map[string]any{"loglevel": "none"}, "inbounds": inbounds, "outbounds": outbounds, "routing": map[string]any{"rules": rules}}
}

func probeHTTPBatch(parent context.Context, jobs []probeJob, mode string, complete func(probeJob, ProbeResult)) {
	probeHTTPBatchURLs(parent, jobs, mode, probeTestURL, probeTraceURL, complete)
}

func probeHTTPBatchURLs(parent context.Context, jobs []probeJob, mode, testURL, traceURL string, complete func(probeJob, ProbeResult)) {
	if len(jobs) == 0 || parent.Err() != nil {
		return
	}
	err := withSharedProbe(parent, jobs, func(ctx context.Context, ports []int) {
		// Each worker owns its transport; keep-alive is reused only within its node.
		indexed := make([]probeJob, len(jobs))
		for i := range jobs {
			indexed[i] = probeJob{ID: strconv.Itoa(i)}
		}
		parallelProbes(ctx, indexed, probeHTTPChunk, func(index probeJob) {
			i, _ := strconv.Atoi(index.ID)
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(ports[i]))
			r := probeHTTPThroughSOCKS(ctx, address, mode, testURL, traceURL)
			r.UDP = jobs[i].Outbound["protocol"] == "hysteria"
			complete(jobs[i], r)
		})
	})
	if err == nil || parent.Err() != nil {
		return
	}
	// A core-incompatible node must not poison the other members of its chunk.
	if len(jobs) > 1 {
		middle := len(jobs) / 2
		probeHTTPBatchURLs(parent, jobs[:middle], mode, testURL, traceURL, complete)
		probeHTTPBatchURLs(parent, jobs[middle:], mode, testURL, traceURL, complete)
		return
	}
	complete(jobs[0], probeError(mode, err.Error()))
}

func withSharedProbe(parent context.Context, jobs []probeJob, work func(context.Context, []int)) error {
	ports := make([]int, len(jobs))
	var reserved []net.Listener
	closePorts := func() {
		for _, l := range reserved {
			l.Close()
		}
	}
	defer closePorts()
	for i := range jobs {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("Не удалось выделить порт проверки")
		}
		reserved = append(reserved, l)
		ports[i] = l.Addr().(*net.TCPAddr).Port
	}
	dir, err := os.MkdirTemp(filepath.Join(stateDir(), "db"), ".probe-")
	if err != nil {
		return fmt.Errorf("Не удалось подготовить проверку")
	}
	defer os.RemoveAll(dir)
	b, err := json.Marshal(sharedProbeConfig(jobs, ports))
	if err != nil {
		return fmt.Errorf("Ошибка подготовки конфигурации проверки")
	}
	path := filepath.Join(dir, "config.json")
	if os.WriteFile(path, b, 0600) != nil {
		return fmt.Errorf("Ошибка записи конфигурации проверки")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	testCtx, testCancel := context.WithTimeout(ctx, 5*time.Second)
	defer testCancel()
	if exec.CommandContext(testCtx, "/usr/local/bin/xray", "run", "-test", "-c", path).Run() != nil {
		return fmt.Errorf("Конфигурация отклонена Xray")
	}
	closePorts()
	process := exec.CommandContext(ctx, "/usr/local/bin/xray", "run", "-c", path)
	if process.Start() != nil {
		return fmt.Errorf("Не удалось запустить тестовый Xray")
	}
	exited := make(chan error, 1)
	go func() { exited <- process.Wait() }()
	defer func() { process.Process.Kill(); <-exited }()
	readyCtx, readyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readyCancel()
	for _, port := range ports {
		for {
			c, e := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(readyCtx, "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if e == nil {
				c.Close()
				break
			}
			select {
			case <-readyCtx.Done():
				return fmt.Errorf("Тестовый Xray не запустился")
			case <-time.After(25 * time.Millisecond):
			}
		}
	}
	work(ctx, ports)
	return nil
}

func probeHTTPThroughSOCKS(ctx context.Context, address, mode, testURL, traceURL string) ProbeResult {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, target string) (net.Conn, error) {
		return socksConnect(ctx, address, target)
	}, MaxIdleConns: 2, MaxIdleConnsPerHost: 1}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return probeHTTPRequests(ctx, client, mode, testURL, traceURL)
}

func probeHTTPRequests(ctx context.Context, client *http.Client, mode, testURL, traceURL string) ProbeResult {
	r := probeError(mode, "HTTPS через VPN не прошёл")
	get := func(ctx context.Context, target string) (int64, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
		if err != nil {
			return 0, nil, err
		}
		start := time.Now()
		resp, err := client.Do(req)
		delay := max(time.Since(start).Milliseconds(), 1)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		return delay, body, err
	}
	delay, _, err := get(ctx, testURL)
	if err != nil {
		return r
	}
	r.State, r.HTTPSMS, r.Message = "ok", delay, "HTTPS через VPN работает"
	if mode == "http" {
		if warm, _, err := get(ctx, testURL); err == nil {
			r.HTTPSMS = warm
		}
		r.Message = "HTTP через VPN работает; задержка запроса по готовому соединению"
		traceCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if _, body, err := get(traceCtx, traceURL); err == nil {
			r.ExitIP, r.Country = traceEgress(body)
		}
	}
	return r
}
