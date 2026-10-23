package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Share expensive home-page collection across reloads, without caching edits,
// query-specific views or the independently refreshed traffic endpoints.
type statusResponseCache struct {
	mu         sync.Mutex
	ttl        time.Duration
	now        func() time.Time
	data       []byte
	at         time.Time
	generation uint64
	loading    chan struct{}
}

func cacheableStatusRequest(r *http.Request) bool {
	query := r.URL.Query().Encode()
	return r.Method == http.MethodGet && (query == "" || query == "tab=status")
}

func (c *statusResponseCache) invalidate() {
	c.mu.Lock()
	c.generation++
	c.data = nil
	c.mu.Unlock()
}

func (c *statusResponseCache) load(ctx context.Context, build func() ([]byte, error)) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		if c.data != nil && c.now().Sub(c.at) < c.ttl {
			data := c.data
			c.mu.Unlock()
			return data, nil
		}
		if done := c.loading; done != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				continue
			}
		}
		done, generation := make(chan struct{}), c.generation
		c.loading = done
		c.mu.Unlock()
		var data []byte
		var err error
		func() {
			defer func() {
				c.mu.Lock()
				if err == nil && len(data) > 0 && generation == c.generation {
					c.data, c.at = data, c.now()
				}
				c.loading = nil
				close(done)
				c.mu.Unlock()
			}()
			data, err = build()
		}()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return data, err
	}
}
