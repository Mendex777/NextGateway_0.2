package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"time"
)

// Config bytes and applied network determine the accounting schema. Do not
// parse and render an unchanged Xray configuration on every sample.
type trafficConfigCache struct {
	raw     []byte
	network GatewayNetwork
	exits   []trafficExit
	want    string
}

func (c *trafficConfigCache) load(n GatewayNetwork, uid string) error {
	raw, err := os.ReadFile("/etc/ngpanel/config.json")
	if err != nil {
		return err
	}
	if c.want != "" && c.network == n && bytes.Equal(raw, c.raw) {
		return nil
	}
	exits, err := parseTrafficExits(raw)
	if err != nil {
		return err
	}
	_, want, err := trafficNFT(n, uid, exits...)
	if err != nil {
		return err
	}
	c.raw, c.network, c.exits, c.want = raw, n, exits, want
	return nil
}

type trafficAggregateReader struct {
	netlink trafficNetlink
	retry   time.Time
}

func (r *trafficAggregateReader) read(ctx context.Context, c *trafficConfigCache, uid, boot string) (map[string]trafficRaw, string, error) {
	if !time.Now().Before(r.retry) {
		counts, gen, err := r.netlink.read(ctx, c.want, boot, c.exits, false)
		if err == nil {
			return counts, gen, nil
		}
		log.Printf("Traffic Netlink unavailable; using nft compatibility reader: %v", err)
		r.retry = time.Now().Add(30 * time.Second)
	}
	// Retains installation/schema repair and compatibility with older kernels.
	return readTrafficNFT(ctx, c.network, uid, boot, c.exits...)
}

type trafficDetailJob struct {
	config    trafficConfigCache
	boot, pid string
}
type trafficDetailResult struct {
	at              time.Time
	generation, pid string
	routes          []TrafficRoute
	err             error
}

func (r trafficDetailResult) valid(now time.Time, generation, pid string) bool {
	age := now.Sub(r.at)
	return r.err == nil && !r.at.IsZero() && age >= 0 && age <= 6*time.Second && r.generation == generation && r.pid == pid
}

// A single worker and an unbuffered request channel bound both memory and
// concurrency. Its snapshots are immutable; a slow flow dump never blocks the
// one-second aggregate counters. Detailed rates use their own sample interval.
func runTrafficDetails(ctx context.Context, jobs <-chan trafficDetailJob, results chan<- trafficDetailResult) {
	reader := trafficNetlink{}
	defer reader.close()
	routes := trafficRouteTracker{}
	udp := udpRouteTracker{}
	lookup := asyncUDPLookup{now: time.Now, lookup: lookupUDPRoutes}
	var retry time.Time
	var coneConfig []byte
	conePID := ""
	cone := false
	for {
		var job trafficDetailJob
		select {
		case <-ctx.Done():
			return
		case job = <-jobs:
		}
		at := time.Now().UTC()
		call, cancel := context.WithTimeout(ctx, 2*time.Second)
		var raw map[string]trafficRaw
		var gen string
		var err error
		if !time.Now().Before(retry) {
			raw, gen, err = reader.read(call, job.config.want, job.boot, job.config.exits, true)
			if err != nil {
				retry = time.Now().Add(30 * time.Second)
			}
		} else {
			err = fmt.Errorf("Netlink retry deferred")
		}
		if err != nil {
			// Read-only fallback: only the aggregate loop may install/repair rules.
			data, e := trafficCommand(call, "", "nft", "-j", "-a", "list", "table", "inet", "ngpanel_traffic")
			if e == nil {
				var handle, comment string
				raw, handle, comment, e = parseTrafficNFT(data)
				if e == nil && comment != job.config.want {
					e = fmt.Errorf("accounting schema changed")
				}
				gen = job.boot + "/nft/" + handle
			}
			err = e
		}
		cancel()
		result := trafficDetailResult{at: at, generation: gen, pid: job.pid, err: err}
		if err == nil {
			if conePID != job.pid || !bytes.Equal(coneConfig, job.config.raw) {
				cone = readTrafficCone(job.config.raw)
				conePID = job.pid
				coneConfig = job.config.raw
			}
			events := readTrafficConnections(at)
			routeEvents := make([]ConnectionEvent, 0, len(events))
			for _, event := range events {
				if event.Network != "udp" {
					routeEvents = append(routeEvents, event)
				}
			}
			routeEvents = append(routeEvents, udp.update(raw, gen, job.pid, job.boot, events, at, cone, lookup.read)...)
			result.routes = routes.update(raw, gen+"/"+job.pid, routeEvents, at, job.config.exits)
		}
		select {
		case results <- result:
		case <-ctx.Done():
			return
		}
	}
}
