package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStatusCacheSharesReloadBurst(t *testing.T) {
	c := statusResponseCache{ttl: time.Second, now: time.Now}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	build := func() ([]byte, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return []byte("snapshot"), nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := c.load(context.Background(), build)
			if err != nil || string(data) != "snapshot" {
				t.Errorf("unexpected snapshot %q: %v", data, err)
			}
		}()
	}
	<-entered
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("burst performed %d collections", calls.Load())
	}
}

func TestStatusCacheExpiresAndInvalidatesRunningCollection(t *testing.T) {
	now := time.Now()
	c := statusResponseCache{ttl: time.Second, now: func() time.Time { return now }}
	load := func(value string) string {
		data, err := c.load(context.Background(), func() ([]byte, error) { return []byte(value), nil })
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if load("first") != "first" || load("cached") != "first" {
		t.Fatal("snapshot not reused")
	}
	now = now.Add(time.Second)
	if load("expired") != "expired" {
		t.Fatal("expired snapshot reused")
	}
	c.invalidate()
	if load("saved") != "saved" {
		t.Fatal("saved changes hidden")
	}
	c.invalidate()
	_, err := c.load(context.Background(), func() ([]byte, error) {
		c.invalidate() // An edit completed while the previous collection ran.
		return []byte("old"), nil
	})
	if err != nil || load("fresh") != "fresh" {
		t.Fatal("in-flight collection repopulated invalidated cache")
	}
}

func TestStatusCacheCancelledWaiterAndFailedBuild(t *testing.T) {
	c := statusResponseCache{ttl: time.Second, now: time.Now}
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = c.load(context.Background(), func() ([]byte, error) { close(entered); <-release; return []byte("ready"), nil })
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.load(ctx, func() ([]byte, error) { t.Error("cancelled waiter collected state"); return nil, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	<-finished
	c.invalidate()
	if _, err := c.load(context.Background(), func() ([]byte, error) { return nil, errors.New("failed") }); err == nil {
		t.Fatal("failure lost")
	}
	data, err := c.load(context.Background(), func() ([]byte, error) { return []byte("recovered"), nil })
	if err != nil || string(data) != "recovered" {
		t.Fatal("failed collection blocked future reloads")
	}
}

func TestStatusCacheOnlyPlainHomeRequests(t *testing.T) {
	for path, want := range map[string]bool{"/api/page": true, "/api/page?tab=status": true, "/api/page?tab=status&setup=1": false, "/api/page?tab=status&message=saved": false, "/api/page?tab=routing": false, "/api/page?tab=status&tab=routing": false} {
		if got := cacheableStatusRequest(httptest.NewRequest("GET", path, nil)); got != want {
			t.Errorf("%s: got %v", path, got)
		}
	}
	if cacheableStatusRequest(httptest.NewRequest("POST", "/api/page", nil)) {
		t.Fatal("mutation cached")
	}
}
