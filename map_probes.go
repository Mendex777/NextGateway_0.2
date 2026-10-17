package main

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type MapProbe struct {
	ID, Outbound string
	Time         time.Time
	Success      bool
}

var mapProbes = struct {
	sync.Mutex
	seen   map[string]int64
	events []MapProbe
}{seen: map[string]int64{}}

func recordMapProbe(tag, id string, success bool, at time.Time) {
	mapProbes.Lock()
	defer mapProbes.Unlock()
	cutoff := time.Now().Add(-time.Minute)
	kept := mapProbes.events[:0]
	for _, e := range mapProbes.events {
		if e.Time.After(cutoff) {
			kept = append(kept, e)
		}
	}
	for _, e := range kept {
		if e.ID == id {
			return
		}
	}
	mapProbes.events = append(kept, MapProbe{ID: id, Outbound: tag, Time: at, Success: success})
	if len(mapProbes.events) > 256 {
		mapProbes.events = mapProbes.events[len(mapProbes.events)-256:]
	}
}
func observeMapProbes(samples map[string]GroupSample) {
	now := time.Now()
	for tag, s := range samples {
		mapProbes.Lock()
		old := mapProbes.seen[tag]
		mapProbes.seen[tag] = s.Checked
		mapProbes.Unlock()
		if s.Checked > old && s.Checked > 0 && now.Sub(time.Unix(s.Checked, 0)) < 10*time.Second {
			recordMapProbe(tag, "observation:"+tag+":"+strconv.FormatInt(s.Checked, 10), s.Alive, time.Unix(s.Checked, 0))
		}
	}
}
func recordNodeMapProbe(node string, success bool, at time.Time) {
	raw, e := os.ReadFile(stateDir() + "/applied-config.json")
	if e != nil {
		return
	}
	var config struct{ Outbounds []struct{ Tag string } }
	if json.Unmarshal(raw, &config) != nil {
		return
	}
	for _, o := range config.Outbounds {
		if o.Tag == "node-"+node || (strings.HasPrefix(o.Tag, "auto-vpn-") && strings.HasSuffix(o.Tag, "-"+node+"-")) {
			recordMapProbe(o.Tag, "manual:"+o.Tag+":"+strconv.FormatInt(at.UnixNano(), 10), success, at)
		}
	}
}
func mapProbeSnapshot() []MapProbe {
	mapProbes.Lock()
	defer mapProbes.Unlock()
	out := []MapProbe{}
	cutoff := time.Now().Add(-time.Minute)
	for _, e := range mapProbes.events {
		if e.Time.After(cutoff) {
			out = append(out, e)
		}
	}
	return out
}
