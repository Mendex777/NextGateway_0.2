package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

func policyFixture() (BalanceGroup, map[string]GroupSample, groupDecision, time.Time) {
	now := time.Unix(1800000000, 0)
	g := BalanceGroup{ID: "7", Nodes: []string{"1", "2"}, Interval: 30, Mode: "threshold", ThresholdMS: 1000, Failures: 2, Cooldown: 60}
	a := "auto-vpn-7-1-"
	b := "auto-vpn-7-2-"
	all := map[string]GroupSample{a: {Tag: a, Alive: true, DelayMS: 800, Checked: now.Unix()}, b: {Tag: b, Alive: true, DelayMS: 50, Checked: now.Unix()}}
	return g, all, groupDecision{Current: a, Switched: now.Add(-time.Minute)}, now
}
func TestThresholdPolicy(t *testing.T) {
	g, all, state, now := policyFixture()
	next := decideGroup(g, all, state, now)
	if next.Current != state.Current {
		t.Fatal("below threshold switched")
	}
	a := all[state.Current]
	a.DelayMS = 1100
	a.Checked++
	all[state.Current] = a
	next = decideGroup(g, all, next, now)
	if next.Bad != 1 {
		t.Fatal("first exceedance missing")
	}
	next = decideGroup(g, all, next, now.Add(time.Second))
	if next.Bad != 1 || next.Current != state.Current {
		t.Fatal("same sample counted twice")
	}
	a.Checked++
	all[state.Current] = a
	next = decideGroup(g, all, next, now.Add(2*time.Second))
	if next.Current != "auto-vpn-7-2-" {
		t.Fatal("threshold did not switch")
	}
}
func TestCooldownFailureAndAllDown(t *testing.T) {
	g, all, state, now := policyFixture()
	state.Switched = now
	state.Bad = 1
	a := all[state.Current]
	a.DelayMS = 2000
	all[state.Current] = a
	next := decideGroup(g, all, state, now)
	if next.Current != state.Current {
		t.Fatal("cooldown ignored")
	}
	a.Alive = false
	a.Checked++
	all[state.Current] = a
	next = decideGroup(g, all, next, now.Add(time.Second))
	if next.Current != "auto-vpn-7-2-" {
		t.Fatal("failure did not bypass cooldown")
	}
	b := all[next.Current]
	b.Alive = false
	all[next.Current] = b
	next = decideGroup(g, all, next, now.Add(2*time.Second))
	if next.Current != "block" {
		t.Fatal("all down not blocked")
	}
	b.Alive = true
	b.Checked = now.Unix() + 3
	all[b.Tag] = b
	next = decideGroup(g, all, next, now.Add(3*time.Second))
	if next.Current != b.Tag {
		t.Fatal("recovery missing")
	}
}
func TestFailureOnlyNoLatencySwitch(t *testing.T) {
	g, all, state, now := policyFixture()
	g.Mode = "failover"
	a := all[state.Current]
	a.DelayMS = 3000
	all[state.Current] = a
	if decideGroup(g, all, state, now).Current != state.Current {
		t.Fatal("failover reacted to latency")
	}
	if decideGroup(g, all, state, now.Add(200*time.Second)).Current != "block" {
		t.Fatal("stale data not blocked")
	}
}
func TestNoFasterCandidateAndThresholdBoundary(t *testing.T) {
	g, all, state, now := policyFixture()
	a := all[state.Current]
	a.DelayMS = 1000
	all[state.Current] = a
	state.Bad = 2
	if decideGroup(g, all, state, now).Bad != 0 {
		t.Fatal("threshold boundary treated as exceedance")
	}
	a.DelayMS = 2000
	a.Checked++
	all[a.Tag] = a
	b := all["auto-vpn-7-2-"]
	b.DelayMS = 2500
	all[b.Tag] = b
	state.Bad = 3
	if decideGroup(g, all, state, now).Current != a.Tag {
		t.Fatal("switched to slower candidate")
	}
}
func TestObservationWireParser(t *testing.T) {
	item := protowire.AppendTag(nil, 1, protowire.VarintType)
	item = protowire.AppendVarint(item, 1)
	item = protowire.AppendTag(item, 2, protowire.VarintType)
	item = protowire.AppendVarint(item, 345)
	item = protowire.AppendTag(item, 4, protowire.BytesType)
	item = protowire.AppendString(item, "tag")
	item = protowire.AppendTag(item, 6, protowire.VarintType)
	item = protowire.AppendVarint(item, 100)
	result := protowire.AppendTag(nil, 1, protowire.BytesType)
	result = protowire.AppendBytes(result, item)
	raw := protowire.AppendTag(nil, 1, protowire.BytesType)
	raw = protowire.AppendBytes(raw, result)
	samples, e := parseObservations(raw)
	if e != nil || !samples["tag"].Alive || samples["tag"].DelayMS != 345 || samples["tag"].Checked != 100 {
		t.Fatal(samples, e)
	}
	if _, e = parseObservations([]byte{10, 255}); e == nil {
		t.Fatal("malformed wire accepted")
	}
}
func TestRealObservatoryAndOverrideAPI(t *testing.T) {
	if os.Getenv("NG_XRAY_INTEGRATION") != "1" {
		t.Skip("real Xray integration disabled")
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := listener.Addr().String()
	listener.Close()
	old := groupAPIAddress
	groupAPIAddress = address
	defer func() { groupAPIAddress = old }()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer origin.Close()
	c := map[string]any{"log": map[string]any{"loglevel": "none"}, "api": map[string]any{"tag": "api", "listen": address, "services": []string{"RoutingService", "ObservatoryService"}}, "outbounds": []any{map[string]any{"tag": "auto-vpn-7-1-", "protocol": "freedom"}, map[string]any{"tag": "auto-vpn-7-2-", "protocol": "freedom"}, map[string]any{"tag": "block", "protocol": "blackhole"}}, "routing": map[string]any{"balancers": []any{map[string]any{"tag": "group-7", "selector": []string{"auto-vpn-7-"}, "fallbackTag": "block", "strategy": map[string]any{"type": "leastPing"}}}}, "observatory": map[string]any{"subjectSelector": []string{"auto-vpn-7-"}, "probeUrl": origin.URL, "probeInterval": "1s", "enableConcurrency": true}}
	raw, _ := json.Marshal(c)
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, raw, 0600)
	cmd := exec.Command("/usr/local/bin/xray", "run", "-c", path)
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	pid := cmd.Process.Pid
	var samples map[string]GroupSample
	for i := 0; i < 15; i++ {
		samples, e = observations()
		if e == nil && len(samples) == 2 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if e != nil || len(samples) != 2 || !samples["auto-vpn-7-1-"].Alive {
		t.Fatal("live observation unavailable", samples, e)
	}
	for _, target := range []string{"auto-vpn-7-1-", "auto-vpn-7-2-", "block"} {
		if e = overrideGroup("7", target); e != nil {
			t.Fatal(e)
		}
		response, e := exec.Command("/usr/local/bin/xray", "api", "bi", "-json", "-s="+address, "group-7").Output()
		if e != nil {
			t.Fatal(e)
		}
		var info struct {
			Balancer struct{ Override struct{ Target string } }
		}
		json.Unmarshal(response, &info)
		if info.Balancer.Override.Target != target {
			t.Fatal("override missing", strconv.Itoa(pid), string(response))
		}
	}
}

func TestGroupPolicyTemplate(t *testing.T) {
	g := BalanceGroup{ID: "7", Name: "Example", Mode: "threshold", ThresholdMS: 1200, Failures: 3, Cooldown: 90, Interval: 30, Nodes: []string{"1", "2"}}
	var out bytes.Buffer
	if e := pageTemplate().Execute(&out, Page{Tab: "subscriptions", Groups: []BalanceGroup{g}, EditGroup: &g}); e != nil {
		t.Fatal(e)
	}
	for _, needle := range []string{"Задержка HTTPS", "name=\"threshold_ms\"", "value=\"1200\"", "group-current", "threshold"} {
		if !strings.Contains(out.String(), needle) {
			t.Fatal("template missing", needle)
		}
	}
}
func TestPolicyPendingUsesAppliedSnapshot(t *testing.T) {
	configDatabase(t)
	g := BalanceGroup{ID: "7", Name: "Example", Mode: "threshold", ThresholdMS: 1000, Failures: 2, Cooldown: 60, Nodes: []string{"1", "2"}, Interval: 30}
	saveGroups([]BalanceGroup{g})
	c := map[string]any{"routing": map[string]any{"balancers": []any{map[string]any{"tag": "group-7"}}}}
	if !groupPolicyPending(c, nil) {
		t.Fatal("unapplied threshold not detected")
	}
	if groupPolicyPending(c, []BalanceGroup{g}) {
		t.Fatal("applied threshold shown as pending")
	}
	g.ThresholdMS = 2000
	saveGroups([]BalanceGroup{g})
	g.ThresholdMS = 1000
	if !groupPolicyPending(c, []BalanceGroup{g}) {
		t.Fatal("threshold change not detected")
	}
}
