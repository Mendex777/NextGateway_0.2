package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Rates for an observed client tuple joined to the actual Xray access event.
// Empty Outbound means only the device-to-gateway leg is known.
type TrafficRoute struct {
	Source, Outbound string
	UpBPS, DownBPS   float64
}
type trafficRouteSample struct {
	raw   trafficRaw
	seen  time.Time
	route string
}
type trafficRouteTracker struct {
	samples    map[string]trafficRouteSample
	generation string
	at         time.Time
}

func trafficTuple(source, sp, dest, dp, network string) string {
	a, b := net.ParseIP(source), net.ParseIP(dest)
	if a == nil || b == nil || a.To4() == nil || b.To4() == nil || (network != "tcp" && network != "udp") {
		return ""
	}
	x, e := strconv.ParseUint(sp, 10, 16)
	if e != nil || x == 0 {
		return ""
	}
	y, e := strconv.ParseUint(dp, 10, 16)
	if e != nil || y == 0 {
		return ""
	}
	return strings.Join([]string{a.To4().String(), strconv.FormatUint(x, 10), b.To4().String(), strconv.FormatUint(y, 10), network}, "|")
}
func trafficTupleValue(raw json.RawMessage) (string, error) {
	var value struct {
		Concat []json.RawMessage `json:"concat"`
	}
	if err := json.Unmarshal(raw, &value); err != nil || len(value.Concat) != 5 {
		return "", fmt.Errorf("invalid traffic tuple")
	}
	parts := make([]string, 5)
	for i, v := range value.Concat {
		if err := json.Unmarshal(v, &parts[i]); err != nil {
			var n uint64
			if json.Unmarshal(v, &n) != nil {
				return "", fmt.Errorf("invalid tuple component")
			}
			parts[i] = strconv.FormatUint(n, 10)
		}
	}
	if parts[4] == "6" {
		parts[4] = "tcp"
	}
	if parts[4] == "17" {
		parts[4] = "udp"
	}
	key := trafficTuple(parts[0], parts[1], parts[2], parts[3], parts[4])
	if key == "" {
		return "", fmt.Errorf("invalid tuple endpoint")
	}
	return key, nil
}
func readTrafficConnections(now time.Time) []ConnectionEvent {
	raw, err := os.ReadFile("/run/ngpanel-connections/events.json")
	if err != nil {
		// The journal file is owned by the panel (0600). The restricted accounting
		// service deliberately has no DAC override; use the same sanitized local API.
		client := http.Client{Timeout: 750 * time.Millisecond}
		response, e := client.Get("http://127.0.0.1:8080/api/connections?raw=1")
		if e != nil {
			return nil
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return nil
		}
		raw, e = io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
		if e != nil {
			return nil
		}
	}
	var snapshot ConnectionSnapshot
	if json.Unmarshal(raw, &snapshot) != nil || now.Sub(snapshot.Updated) > 5*time.Second || snapshot.Updated.After(now.Add(time.Second)) {
		return nil
	}
	return snapshot.Events
}
func (t *trafficRouteTracker) update(raw map[string]trafficRaw, generation string, events []ConnectionEvent, now time.Time, exits []trafficExit) []TrafficRoute {
	if t.generation != generation {
		t.samples = map[string]trafficRouteSample{}
		t.at = time.Time{}
		t.generation = generation
	}
	if t.samples == nil {
		t.samples = map[string]trafficRouteSample{}
	}
	allowed := map[string]bool{}
	for _, e := range exits {
		allowed[e.Tag] = true
	}
	candidates := map[string]map[string]bool{}
	for _, e := range events {
		if e.Time.After(now.Add(time.Second)) || e.Inbound != "tproxy-in" || !allowed[e.Outbound] {
			continue
		}
		// UDP bindings are validated by udpRouteTracker; TCP uses active/recent sockets.
		if e.Network != "udp" && e.TCPState != "active" && now.Sub(e.Time) > 90*time.Second && (e.ClosedAt == nil || now.Sub(*e.ClosedAt) > 90*time.Second) {
			continue
		}
		key := trafficTuple(e.Source, e.SourcePort, e.Destination, e.Port, e.Network)
		if key == "" {
			continue
		}
		if candidates[key] == nil {
			candidates[key] = map[string]bool{}
		}
		candidates[key][e.Outbound] = true
	}
	seconds := now.Sub(t.at).Seconds()
	valid := !t.at.IsZero() && seconds > 0 && seconds <= 10
	grouped := map[string]TrafficRoute{}
	for full, count := range raw {
		if !strings.HasPrefix(full, "flow:") {
			continue
		}
		key := strings.TrimPrefix(full, "flow:")
		parts := strings.Split(key, "|")
		if len(parts) != 5 {
			continue
		}
		old, existed := t.samples[key]
		route := old.route
		choices := candidates[key]
		if parts[4] == "udp" && len(choices) != 1 {
			route = ""
		}
		if len(choices) > 1 {
			route = ""
		} else if len(choices) == 1 {
			for tag := range choices {
				route = tag
			}
		}
		// A reset/reused tuple cannot inherit a route from its previous connection.
		reset := existed && (count.Up < old.raw.Up || count.Down < old.raw.Down)
		if reset && len(choices) != 1 {
			route = ""
		}
		delta := count
		if existed && !reset {
			delta.Up -= old.raw.Up
			delta.Down -= old.raw.Down
		}
		emittedRoute := route
		if existed && old.route != "" && route != old.route {
			emittedRoute = ""
		}
		pair := parts[0] + "|" + emittedRoute
		v := grouped[pair]
		v.Source = parts[0]
		v.Outbound = emittedRoute
		if valid && !reset {
			v.UpBPS += float64(delta.Up) / seconds
			v.DownBPS += float64(delta.Down) / seconds
		}
		grouped[pair] = v
		t.samples[key] = trafficRouteSample{raw: count, seen: now, route: route}
	}
	for key, v := range t.samples {
		if now.Sub(v.seen) > 5*time.Second {
			delete(t.samples, key)
		}
	}
	t.at = now
	out := make([]TrafficRoute, 0, len(grouped))
	for _, v := range grouped {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source+"|"+out[i].Outbound < out[j].Source+"|"+out[j].Outbound })
	return out
}
