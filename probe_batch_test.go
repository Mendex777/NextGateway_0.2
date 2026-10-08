package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelProbeLimitAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 32)
	release := make(chan struct{})
	done := make(chan struct{})
	jobs := make([]probeJob, 40)
	var calls atomic.Int32
	go func() {
		parallelProbes(ctx, jobs, 8, func(probeJob) { calls.Add(1); started <- struct{}{}; <-release })
		close(done)
	}()
	for i := 0; i < 8; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("workers were not started concurrently")
		}
	}
	select {
	case <-started:
		t.Fatal("worker limit exceeded")
	default:
	}
	cancel()
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not release worker pool")
	}
	if calls.Load() != 8 {
		t.Fatalf("queued work ran after cancellation: %d", calls.Load())
	}
}

func TestHTTPProbeWarmFailureAndTraceFailureDoNotHideReachability(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trace" {
			http.Error(w, "no metadata", 503)
			return
		}
		if count.Add(1) >= 2 {
			panic(http.ErrAbortHandler)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	r := probeHTTPRequests(context.Background(), server.Client(), "http", server.URL, server.URL+"/trace")
	if r.State != "ok" || r.HTTPSMS < 1 || r.ExitIP != "" || r.Country != "" {
		t.Fatalf("successful cold probe was lost: %+v", r)
	}
}

func TestSharedProbeRoutesEachPortAndCleansCore(t *testing.T) {
	if _, err := os.Stat("/usr/local/bin/xray"); err != nil {
		t.Skip("requires installed Xray")
	}
	state := t.TempDir()
	t.Setenv("NG_STATE", state)
	os.Mkdir(filepath.Join(state, "db"), 0700)
	servers := make([]*httptest.Server, 4)
	jobs := make([]probeJob, len(servers))
	for i := range jobs {
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "ip=203.0.113.%d\nloc=NL\n", i+1) }))
		defer servers[i].Close()
		jobs[i] = probeJob{Outbound: map[string]any{"protocol": "freedom", "settings": map[string]any{"redirect": strings.TrimPrefix(servers[i].URL, "http://")}, "streamSettings": map[string]any{}}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := withSharedProbe(ctx, jobs, func(ctx context.Context, ports []int) {
		var wg sync.WaitGroup
		for i, port := range ports {
			wg.Add(1)
			go func(i, port int) {
				defer wg.Done()
				address := fmt.Sprintf("127.0.0.1:%d", port)
				r := probeHTTPThroughSOCKS(ctx, address, "http", "http://127.0.0.1:1", "http://127.0.0.1:1")
				if r.State != "ok" || r.ExitIP != fmt.Sprintf("203.0.113.%d", i+1) || r.Country != "NL" {
					t.Errorf("SOCKS port %d failed: %+v", i, r)
				}
			}(i, port)
		}
		wg.Wait()
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(state, "db"))
	if len(entries) != 0 {
		t.Fatal("shared probe left credentials on disk")
	}
}

func TestBadOutboundDoesNotPoisonSharedProbe(t *testing.T) {
	if _, err := os.Stat("/usr/local/bin/xray"); err != nil {
		t.Skip("requires installed Xray")
	}
	state := t.TempDir()
	t.Setenv("NG_STATE", state)
	os.Mkdir(filepath.Join(state, "db"), 0700)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	jobs := []probeJob{
		{ID: "good", Outbound: map[string]any{"protocol": "freedom", "settings": map[string]any{}, "streamSettings": map[string]any{}}},
		{ID: "bad", Outbound: map[string]any{"protocol": "not-a-protocol", "settings": map[string]any{}, "streamSettings": map[string]any{}}},
	}
	results := map[string]ProbeResult{}
	var mu sync.Mutex
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probeHTTPBatchURLs(ctx, jobs, "real", server.URL, server.URL, func(job probeJob, r ProbeResult) { mu.Lock(); results[job.ID] = r; mu.Unlock() })
	if results["good"].State != "ok" || results["bad"].State != "error" || len(results) != 2 {
		t.Fatalf("bad member affected its neighbor: %+v", results)
	}
	entries, _ := os.ReadDir(filepath.Join(state, "db"))
	if len(entries) != 0 {
		t.Fatal("failed batch left credentials on disk")
	}
}

func TestSharedProbeCancellationReleasesPorts(t *testing.T) {
	if _, err := os.Stat("/usr/local/bin/xray"); err != nil {
		t.Skip("requires installed Xray")
	}
	state := t.TempDir()
	t.Setenv("NG_STATE", state)
	os.Mkdir(filepath.Join(state, "db"), 0700)
	jobs := []probeJob{{Outbound: map[string]any{"protocol": "freedom", "settings": map[string]any{}, "streamSettings": map[string]any{}}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	port := 0
	err := withSharedProbe(ctx, jobs, func(ctx context.Context, ports []int) { port = ports[0]; cancel(); <-ctx.Done() })
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal("cancel left a test listener running:", err)
	}
	l.Close()
	entries, _ := os.ReadDir(filepath.Join(state, "db"))
	if len(entries) != 0 {
		t.Fatal("cancel left credentials on disk")
	}
}
