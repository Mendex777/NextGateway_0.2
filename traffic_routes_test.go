package main

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTrafficRoutesRealTuples(t *testing.T) {
	path := os.Getenv("NG_ROUTE_NFT")
	if path == "" {
		t.Skip("real lab fixture")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	counts, _, _, err := parseTrafficNFT(raw)
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(os.Getenv("NG_ROUTE_LOG"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	events := []ConnectionEvent{}
	for i, line := range strings.Split(string(log), "\n") {
		if e, ok := parseConnection(line, strconv.Itoa(i), now); ok {
			events = append(events, e)
		}
	}
	tracker := trafficRouteTracker{}
	exits := []trafficExit{{Tag: "direct"}, {Tag: "node-1"}}
	tracker.update(map[string]trafficRaw{}, "test", events, now.Add(-time.Second), exits)
	routes := tracker.update(counts, "test", events, now, exits)
	found := map[string]TrafficRoute{}
	for _, r := range routes {
		if r.Source != "198.18.0.2" {
			t.Fatal("foreign source", r)
		}
		found[r.Outbound] = r
	}
	for _, tag := range []string{"direct", "node-1"} {
		r := found[tag]
		if r.UpBPS < 1000000 || r.DownBPS < 1000000 {
			t.Fatal("missing route or direction", tag, r, routes)
		}
	}
}
func TestTrafficRouteSafety(t *testing.T) {
	now := time.Now()
	key := "flow:" + trafficTuple("192.168.1.83", "12345", "203.0.113.2", "443", "tcp")
	exits := []trafficExit{{Tag: "direct"}, {Tag: "vpn"}}
	a := ConnectionEvent{Inbound: "tproxy-in", Source: "192.168.1.83", SourcePort: "12345", Destination: "203.0.113.2", Port: "443", Network: "tcp", Outbound: "direct", Time: now, TCPState: "active"}
	tracker := trafficRouteTracker{}
	tracker.update(map[string]trafficRaw{key: {Up: 10, Down: 20}}, "a", nil, now, exits)
	rows := tracker.update(map[string]trafficRaw{key: {Up: 110, Down: 220}}, "a", []ConnectionEvent{a}, now.Add(time.Second), exits)
	if len(rows) != 1 || rows[0].Outbound != "direct" || rows[0].UpBPS != 100 || rows[0].DownBPS != 200 {
		t.Fatal(rows)
	}
	b := a
	b.Outbound = "vpn"
	rows = tracker.update(map[string]trafficRaw{key: {Up: 210, Down: 420}}, "a", []ConnectionEvent{a, b}, now.Add(2*time.Second), exits)
	if rows[0].Outbound != "" {
		t.Fatal("ambiguous route guessed", rows)
	}
	rows = tracker.update(map[string]trafficRaw{key: {Up: 1, Down: 2}}, "b", nil, now.Add(3*time.Second), exits)
	if rows[0].UpBPS != 0 || rows[0].Outbound != "" {
		t.Fatal("epoch retained route", rows)
	}
	a.SourcePort = "12346"
	rows = tracker.update(map[string]trafficRaw{key: {Up: 100, Down: 200}}, "b", []ConnectionEvent{a}, now.Add(4*time.Second), exits)
	if rows[0].Outbound != "" {
		t.Fatal("joined by IP without matching port", rows)
	}
	tuple, _ := json.Marshal(map[string]any{"concat": []any{"192.168.1.83", 12345, "203.0.113.2", 443, "udp"}})
	if key, err := trafficTupleValue(tuple); err != nil || !strings.HasSuffix(key, "|udp") {
		t.Fatal(key, err)
	}
}
func TestMapProbeEventsDeduplicate(t *testing.T) {
	mapProbes.Lock()
	mapProbes.events = nil
	mapProbes.seen = map[string]int64{}
	mapProbes.Unlock()
	now := time.Now().UTC()
	recordMapProbe("vpn", "test", true, now)
	recordMapProbe("vpn", "test", true, now)
	got := mapProbeSnapshot()
	if len(got) != 1 || got[0].Outbound != "vpn" || !got[0].Success {
		t.Fatal(got)
	}
}
