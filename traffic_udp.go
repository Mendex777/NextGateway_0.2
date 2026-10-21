package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const udpRouteLimit = 4096

type udpRouteBinding struct {
	raw        trafficRaw
	event      ConnectionEvent
	first      time.Time
	retry      time.Time
	historical bool
}
type udpRouteTracker struct {
	epoch    string
	began    time.Time
	bindings map[string]udpRouteBinding
}
type udpRouteLookup func(string, string, []string, bool) ([]ConnectionEvent, error)

func readTrafficCorePID() string {
	raw, err := os.ReadFile("/sys/fs/cgroup/system.slice/xray.service/cgroup.procs")
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 1 {
		return ""
	}
	if n, err := strconv.Atoi(fields[0]); err != nil || n < 1 {
		return ""
	}
	return fields[0]
}

// Recover current-process UDP associations independently of the short public
// connection-history window. Source-only matching requires verified cone mode.
type udpJournalOutput struct{ bytes.Buffer }

func (b *udpJournalOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1024*1024 {
		return 0, fmt.Errorf("UDP journal response exceeds limit")
	}
	return b.Buffer.Write(p)
}
func lookupUDPRoutes(pid, boot string, keys []string, cone bool) ([]ConnectionEvent, error) {
	patterns := []string{}
	for _, key := range keys {
		p := strings.Split(key, "|")
		if len(p) != 5 || p[4] != "udp" {
			continue
		}
		pattern := `from (udp:)?` + regexp.QuoteMeta(p[0]+":"+p[1]) + ` accepted udp:`
		if !cone {
			pattern += regexp.QuoteMeta(p[2]+":"+p[3]) + ` \[`
		}
		patterns = append(patterns, pattern)
	}
	if len(patterns) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "journalctl", "--unit=xray.service", "_PID="+pid, "_BOOT_ID="+strings.ReplaceAll(boot, "-", ""), "--no-pager", "--output=json", "--lines=128", "--grep="+strings.Join(patterns, "|"))
	output := &udpJournalOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return parseUDPJournal(output.Bytes(), pid, boot)
}
func parseUDPJournal(raw []byte, pid, boot string) ([]ConnectionEvent, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	events := []ConnectionEvent{}
	for scanner.Scan() {
		var row struct {
			Message string `json:"MESSAGE"`
			ID      string `json:"__CURSOR"`
			Time    string `json:"__REALTIME_TIMESTAMP"`
			PID     string `json:"_PID"`
			Boot    string `json:"_BOOT_ID"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil || row.PID != pid || strings.ReplaceAll(row.Boot, "-", "") != strings.ReplaceAll(boot, "-", "") {
			continue
		}
		micros, err := strconv.ParseInt(row.Time, 10, 64)
		if err != nil {
			continue
		}
		event, ok := parseConnection(row.Message, row.ID, time.UnixMicro(micros).UTC())
		if ok && event.Network == "udp" && event.Inbound == "tproxy-in" {
			events = append(events, event)
		}
	}
	return events, scanner.Err()
}

func (t *udpRouteTracker) update(raw map[string]trafficRaw, generation, pid, boot string, events []ConnectionEvent, now time.Time, cone bool, lookup udpRouteLookup) []ConnectionEvent {
	if pid == "" {
		t.epoch = ""
		t.bindings = nil
		return nil
	}
	epoch := generation + "/" + pid + "/" + strconv.FormatBool(cone)
	initial := t.epoch != epoch || t.bindings == nil
	if initial {
		t.epoch = epoch
		t.began = now
		t.bindings = map[string]udpRouteBinding{}
	}
	// Remove absent tuples immediately: a later reuse must not inherit an old exit.
	for key := range t.bindings {
		if _, ok := raw["flow:"+key]; !ok {
			delete(t.bindings, key)
		}
	}
	keys := []string{}
	for full := range raw {
		if strings.HasPrefix(full, "flow:") && strings.HasSuffix(full, "|udp") {
			keys = append(keys, strings.TrimPrefix(full, "flow:"))
		}
	}
	sort.Strings(keys)
	pending := []string{}
	latest := map[string]ConnectionEvent{}
	for _, event := range events {
		if event.Network != "udp" || event.Inbound != "tproxy-in" || event.Outbound == "" || event.Time.Before(t.began) || event.Time.After(now.Add(time.Second)) {
			continue
		}
		key := trafficTuple(event.Source, event.SourcePort, event.Destination, event.Port, event.Network)
		key = udpAssociation(key, cone)
		if event.Time.After(latest[key].Time) {
			latest[key] = event
		}
	}
	for _, key := range keys {
		count := raw["flow:"+key]
		entry, exists := t.bindings[key]
		if !exists {
			if len(t.bindings) >= udpRouteLimit {
				continue
			}
			entry = udpRouteBinding{first: now, historical: initial}
			if cone {
				for peerKey, peer := range t.bindings {
					if udpAssociation(peerKey, true) == udpAssociation(key, true) && peer.event.Outbound != "" {
						entry.event = peer.event
						entry.historical = true
					}
				}
			}
		} else if count.Up < entry.raw.Up || count.Down < entry.raw.Down {
			entry = udpRouteBinding{first: now}
		}
		// Newly logged bindings update an existing flow without waiting for backfill.
		// Earlier entries are recovered below with explicit PID/boot verification.
		if event, ok := latest[udpAssociation(key, cone)]; ok && !event.Time.Before(entry.first.Add(-2*time.Second)) && event.Time.After(entry.event.Time) {
			entry.event = event
		}
		active := count.Up > entry.raw.Up || count.Down > entry.raw.Down
		if entry.event.Outbound == "" && active && !now.Before(entry.retry) && len(pending) < 32 {
			pending = append(pending, key)
			entry.retry = now.Add(30 * time.Second)
		}
		entry.raw = count
		t.bindings[key] = entry
	}
	if len(pending) > 0 {
		recovered, err := lookup(pid, boot, pending, cone)
		if err == nil {
			for _, key := range pending {
				entry := t.bindings[key]
				for _, event := range recovered {
					association := trafficTuple(event.Source, event.SourcePort, event.Destination, event.Port, event.Network)
					if udpAssociation(association, cone) != udpAssociation(key, cone) || event.Network != "udp" || event.Inbound != "tproxy-in" || event.Outbound == "" || event.Time.After(now.Add(time.Second)) || (!entry.historical && event.Time.Before(entry.first.Add(-2*time.Second))) {
						continue
					}
					if event.Time.After(entry.event.Time) {
						entry.event = event
					}
				}
				t.bindings[key] = entry
			}
		}
	}
	out := []ConnectionEvent{}
	for key, entry := range t.bindings {
		if entry.event.Outbound != "" {
			event := entry.event
			p := strings.Split(key, "|")
			event.Source = p[0]
			event.SourcePort = p[1]
			event.Destination = p[2]
			event.Port = p[3]
			out = append(out, event)
		}
	}
	return out
}

func udpAssociation(key string, cone bool) string {
	if !cone {
		return key
	}
	p := strings.Split(key, "|")
	if len(p) != 5 {
		return ""
	}
	return p[0] + "|" + p[1] + "|udp"
}

// Source-only association matching is enabled only for the tested Xray core,
// transparent dokodemo inbound and a verifiable service environment.
func readTrafficCone(config []byte) bool {
	var c struct {
		Inbounds []struct {
			Tag, Protocol string
			Settings      struct {
				FollowRedirect bool `json:"followRedirect"`
			}
		}
	}
	if json.Unmarshal(config, &c) != nil {
		return false
	}
	found := false
	for _, in := range c.Inbounds {
		if in.Tag == "tproxy-in" && in.Protocol == "dokodemo-door" && in.Settings.FollowRedirect {
			found = true
		}
	}
	if !found {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, "/usr/local/bin/xray", "version").Output()
	if err != nil || !strings.HasPrefix(string(version), "Xray 26.3.27 ") {
		return false
	}
	env, err := exec.CommandContext(ctx, "systemctl", "show", "xray", "--property=Environment", "--value").Output()
	if err != nil {
		return false
	}
	files, err := exec.CommandContext(ctx, "systemctl", "show", "xray", "--property=EnvironmentFiles", "--value").Output()
	if err != nil || len(bytes.TrimSpace(files)) > 0 {
		return false
	}
	manager, err := exec.CommandContext(ctx, "systemctl", "show-environment").Output()
	if err != nil {
		return false
	}
	env = append(env, append([]byte(" "), manager...)...)
	for _, field := range strings.Fields(string(env)) {
		field = strings.Trim(field, "\"")
		if field == "XRAY_CONE_DISABLED=true" || field == "xray.cone.disabled=true" {
			return false
		}
	}
	return true
}
