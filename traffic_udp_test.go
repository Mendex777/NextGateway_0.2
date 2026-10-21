package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func udpFixture(now time.Time) (ConnectionEvent, string) {
	e := ConnectionEvent{ID: "access", Time: now.Add(-time.Hour), Source: "192.168.1.83", SourcePort: "12345", Destination: "203.0.113.2", Port: "3478", Network: "udp", Inbound: "tproxy-in", Outbound: "direct"}
	return e, trafficTuple(e.Source, e.SourcePort, e.Destination, e.Port, e.Network)
}
func TestUDPLongFlowRecoveryAndConeDestination(t *testing.T) {
	now := time.Now()
	event, _ := udpFixture(now)
	media := trafficTuple(event.Source, event.SourcePort, "203.0.113.3", "21076", "udp")
	for _, cone := range []bool{false, true} {
		tracker := udpRouteTracker{}
		calls := 0
		lookup := func(pid, boot string, keys []string, gotCone bool) ([]ConnectionEvent, error) {
			calls++
			if pid != "123" || gotCone != cone {
				t.Fatal(pid, gotCone)
			}
			return []ConnectionEvent{event}, nil
		}
		raw := map[string]trafficRaw{"flow:" + media: {Down: 100}}
		got := tracker.update(raw, "a", "123", "boot", nil, now, cone, lookup)
		if cone {
			if len(got) != 1 || got[0].Outbound != "direct" || got[0].Destination != "203.0.113.3" {
				t.Fatal(got)
			}
		} else if len(got) != 0 {
			t.Fatal("Source-port fallback used without cone verification", got)
		}
		if calls != 1 {
			t.Fatal(calls)
		}
		if cone {
			for i := 1; i <= 1200; i++ {
				raw["flow:"+media] = trafficRaw{Down: uint64(100 + i)}
				got = tracker.update(raw, "a", "123", "boot", nil, now.Add(time.Duration(i)*time.Second), cone, lookup)
				if len(got) != 1 || got[0].Outbound != "direct" {
					t.Fatal("Long flow lost binding", i, got)
				}
			}
			if calls != 1 {
				t.Fatal("Known flow repeatedly scanned journal", calls)
			}
		}
	}
}
func TestUDPReuseResetAndProcessRestart(t *testing.T) {
	now := time.Now()
	event, key := udpFixture(now)
	tracker := udpRouteTracker{}
	lookup := func(string, string, []string, bool) ([]ConnectionEvent, error) { return []ConnectionEvent{event}, nil }
	raw := map[string]trafficRaw{"flow:" + key: {Down: 100}}
	if len(tracker.update(raw, "a", "123", "boot", nil, now, true, lookup)) != 1 {
		t.Fatal("Initial recovery failed")
	}
	tracker.update(map[string]trafficRaw{}, "a", "123", "boot", nil, now.Add(time.Second), true, lookup)
	if got := tracker.update(raw, "a", "123", "boot", nil, now.Add(2*time.Second), true, lookup); len(got) != 0 {
		t.Fatal("Reused port inherited stale association", got)
	}
	fresh := event
	fresh.Time = now.Add(3 * time.Second)
	fresh.Outbound = "vpn"
	if got := tracker.update(raw, "a", "123", "boot", []ConnectionEvent{fresh}, fresh.Time, true, lookup); len(got) != 1 || got[0].Outbound != "vpn" {
		t.Fatal(got)
	}
	raw["flow:"+key] = trafficRaw{Down: 1}
	if got := tracker.update(raw, "a", "123", "boot", nil, now.Add(10*time.Second), true, lookup); len(got) != 0 {
		t.Fatal("Counter reset kept old binding", got)
	}
	empty := func(string, string, []string, bool) ([]ConnectionEvent, error) { return nil, nil }
	if got := tracker.update(raw, "a", "999", "boot", nil, now.Add(11*time.Second), true, empty); len(got) != 0 {
		t.Fatal("Core restart retained binding", got)
	}
	if got := tracker.update(raw, "a", "", "boot", nil, now.Add(12*time.Second), true, lookup); len(got) != 0 {
		t.Fatal("Unknown core process guessed", got)
	}
}
func TestUDPJournalProcessProvenance(t *testing.T) {
	now := time.Now()
	event, _ := udpFixture(now)
	row := map[string]string{"MESSAGE": fmt.Sprintf("from %s:%s accepted udp:%s:%s [tproxy-in -> direct]", event.Source, event.SourcePort, event.Destination, event.Port), "__CURSOR": "id", "__REALTIME_TIMESTAMP": fmt.Sprint(now.UnixMicro()), "_PID": "123", "_BOOT_ID": "abcd"}
	raw, _ := json.Marshal(row)
	got, err := parseUDPJournal(raw, "123", "abcd")
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	for _, pair := range [][2]string{{"999", "abcd"}, {"123", "other"}} {
		got, _ = parseUDPJournal(raw, pair[0], pair[1])
		if len(got) != 0 {
			t.Fatal("Foreign process journal accepted", got)
		}
	}
}
func TestUDPBoundedLookupAndRetry(t *testing.T) {
	now := time.Now()
	tracker := udpRouteTracker{}
	raw := map[string]trafficRaw{}
	for i := 1; i <= udpRouteLimit+100; i++ {
		raw["flow:"+trafficTuple("192.168.1.83", fmt.Sprint(i), "203.0.113.2", "443", "udp")] = trafficRaw{Down: 100}
	}
	calls := 0
	lookup := func(_, _ string, keys []string, _ bool) ([]ConnectionEvent, error) {
		calls++
		if len(keys) > 32 {
			t.Fatal(len(keys))
		}
		return nil, fmt.Errorf("journal unavailable")
	}
	tracker.update(raw, "a", "123", "boot", nil, now, false, lookup)
	if len(tracker.bindings) > udpRouteLimit || calls != 1 {
		t.Fatal(len(tracker.bindings), calls)
	}
}

// Real Xray + nft fixture from the disposable network lab. Both associations
// keep their first selected outbound when the packet destination changes.
func TestUDPRealConeAssociations(t *testing.T) {
	path := os.Getenv("NG_UDP_LAB")
	if path == "" {
		t.Skip("isolated Xray lab fixture")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ SourcePort, Destination, Outbound string }
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	nft, err := os.ReadFile(os.Getenv("NG_ROUTE_NFT"))
	if err != nil {
		t.Fatal(err)
	}
	counts, _, _, err := parseTrafficNFT(nft)
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(os.Getenv("NG_ROUTE_LOG"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	events := []ConnectionEvent{}
	for i, line := range strings.Split(string(log), "\n") {
		if e, ok := parseConnection(line, strconv.Itoa(i), now.Add(-time.Hour)); ok {
			events = append(events, e)
		}
	}
	for _, c := range cases {
		key := trafficTuple("198.18.0.2", c.SourcePort, c.Destination, "8082", "udp")
		count, ok := counts["flow:"+key]
		if !ok || count.Down < 1200 {
			t.Fatal("Real UDP tuple missing", c, count)
		}
		for _, e := range events {
			if trafficTuple(e.Source, e.SourcePort, e.Destination, e.Port, e.Network) == key {
				t.Fatal("Fixture accidentally has exact destination log", c)
			}
		}
		tracker := udpRouteTracker{}
		got := tracker.update(map[string]trafficRaw{"flow:" + key: count}, "lab", "123", "boot", nil, now, true, func(string, string, []string, bool) ([]ConnectionEvent, error) { return events, nil })
		if len(got) != 1 || got[0].Outbound != c.Outbound {
			t.Fatal("Real association mismatch", c, got)
		}
		t.Log("Confirmed changed UDP destination", c.SourcePort, c.Destination, "via", c.Outbound)
	}
}

func TestUDPExactLongFlowAndNewConeDestination(t *testing.T) {
	now := time.Now()
	event, key := udpFixture(now)
	lookup := func(string, string, []string, bool) ([]ConnectionEvent, error) { return []ConnectionEvent{event}, nil }
	tracker := udpRouteTracker{}
	raw := map[string]trafficRaw{"flow:" + key: {Up: 100, Down: 1000}}
	if got := tracker.update(raw, "a", "123", "boot", nil, now, false, lookup); len(got) != 1 {
		t.Fatal("Exact long flow was not recovered", got)
	}
	raw["flow:"+key] = trafficRaw{Up: 200, Down: 2000}
	got := tracker.update(raw, "a", "123", "boot", nil, now.Add(20*time.Minute), false, lookup)
	if len(got) != 1 || got[0].Outbound != "direct" {
		t.Fatal(got)
	}
	routes := trafficRouteTracker{}
	exits := []trafficExit{{Tag: "direct"}}
	routes.update(raw, "a", got, now, exits)
	raw["flow:"+key] = trafficRaw{Up: 300, Down: 4000}
	out := routes.update(raw, "a", got, now.Add(time.Second), exits)
	if len(out) != 1 || out[0].Outbound != "direct" || out[0].DownBPS != 2000 {
		t.Fatal("Old UDP timestamp suppressed route", out)
	}
	out = routes.update(raw, "a", nil, now.Add(2*time.Second), exits)
	if len(out) != 1 || out[0].Outbound != "" {
		t.Fatal("Unconfirmed UDP route was retained", out)
	}
	tracker = udpRouteTracker{}
	tracker.update(raw, "a", "123", "boot", nil, now, true, lookup)
	second := trafficTuple(event.Source, event.SourcePort, "203.0.113.9", "443", "udp")
	raw["flow:"+second] = trafficRaw{Down: 1200}
	got = tracker.update(raw, "a", "123", "boot", nil, now.Add(10*time.Minute), true, lookup)
	if len(got) != 2 {
		t.Fatal("New destination lost active cone association", got)
	}
}
