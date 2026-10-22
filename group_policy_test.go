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
	if next.Current != state.Current {
		t.Fatal("confirmed failure ignored cooldown")
	}
	for tag, sample := range all {
		sample.Checked = now.Add(time.Minute).Unix()
		all[tag] = sample
	}
	next = decideGroup(g, all, next, now.Add(time.Minute))
	if next.Current != "auto-vpn-7-2-" {
		t.Fatal("confirmed failure did not switch after cooldown")
	}
	g.Cooldown = 0
	b := all[next.Current]
	b.Alive = false
	b.Checked++
	all[b.Tag] = b
	next = decideGroup(g, all, next, now.Add(61*time.Second))
	if next.Current != b.Tag || next.Bad != 1 {
		t.Fatal("first failure switched to block")
	}
	b.Checked++
	all[b.Tag] = b
	next = decideGroup(g, all, next, now.Add(62*time.Second))
	if next.Current != "block" {
		t.Fatal("confirmed all-down not blocked")
	}
	b.Alive = true
	b.Checked++
	all[b.Tag] = b
	next = decideGroup(g, all, next, now.Add(63*time.Second))
	if next.Current != b.Tag {
		t.Fatal("recovery missing")
	}
}
func TestThresholdFailuresShareCounterAndRecover(t *testing.T) {
	g, all, state, now := policyFixture()
	a := all[state.Current]
	a.Alive = false
	a.DelayMS = 0
	all[a.Tag] = a
	next := decideGroup(g, all, state, now)
	if next.Current != state.Current || next.Bad != 1 {
		t.Fatal("first failed check switched", next)
	}
	next = decideGroup(g, all, next, now.Add(time.Second))
	if next.Bad != 1 {
		t.Fatal("same failure counted twice")
	}
	a.Alive = true
	a.DelayMS = 200
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(2*time.Second))
	if next.Bad != 0 || next.Current != state.Current {
		t.Fatal("success did not reset failure")
	}
	a.DelayMS = 1500
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(3*time.Second))
	a.Alive = false
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(4*time.Second))
	if next.Current != "auto-vpn-7-2-" {
		t.Fatal("mixed exceedance/failure did not switch", next)
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

func TestFailoverCountsFailuresAndIgnoresDelay(t *testing.T) {
	g, all, state, now := policyFixture()
	g.Mode = "failover"
	a := all[state.Current]
	a.DelayMS = 5000
	all[a.Tag] = a
	next := decideGroup(g, all, state, now)
	if next.Bad != 0 || next.Current != a.Tag {
		t.Fatal("latency affected failover")
	}
	a.Alive = false
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(time.Second))
	if next.Bad != 1 || next.Current != a.Tag {
		t.Fatal("first failure switched")
	}
	next = decideGroup(g, all, next, now.Add(2*time.Second))
	if next.Bad != 1 {
		t.Fatal("duplicate failure counted")
	}
	a.Alive = true
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(3*time.Second))
	if next.Bad != 0 {
		t.Fatal("success did not reset")
	}
	a.Alive = false
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(4*time.Second))
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(5*time.Second))
	if next.Current != "auto-vpn-7-2-" {
		t.Fatal("confirmed failure not switched", next)
	}
}
func TestFastestGainRequiresPairedFreshSamples(t *testing.T) {
	g, all, state, now := policyFixture()
	g.Mode = "fastest"
	g.FastestPolicy = "gain"
	g.GainMS = 50
	a := all[state.Current]
	a.DelayMS = 300
	all[a.Tag] = a
	b := all["auto-vpn-7-2-"]
	b.DelayMS = 280
	all[b.Tag] = b
	next := decideGroup(g, all, state, now)
	if next.Bad != 0 {
		t.Fatal("small advantage counted")
	}
	b.DelayMS = 220
	b.Checked++
	all[b.Tag] = b
	next = decideGroup(g, all, next, now.Add(time.Second))
	if next.Bad != 0 {
		t.Fatal("old current sample paired twice")
	}
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(2*time.Second))
	if next.Bad != 1 || next.Current != a.Tag {
		t.Fatal("first confirmation missing", next)
	}
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(3*time.Second))
	if next.Bad != 1 {
		t.Fatal("old candidate counted twice")
	}
	b.Checked++
	all[b.Tag] = b
	next = decideGroup(g, all, next, now.Add(4*time.Second))
	if next.Current != b.Tag {
		t.Fatal("gain not switched", next)
	}
}
func TestFastestGainCandidateMustRemainSame(t *testing.T) {
	g, all, state, now := policyFixture()
	g.Mode = "fastest"
	g.FastestPolicy = "gain"
	g.GainMS = 50
	g.Nodes = append(g.Nodes, "3")
	a := all[state.Current]
	a.DelayMS = 300
	all[a.Tag] = a
	b := all["auto-vpn-7-2-"]
	b.DelayMS = 200
	all[b.Tag] = b
	c := GroupSample{Tag: "auto-vpn-7-3-", Alive: true, DelayMS: 250, Checked: now.Unix()}
	all[c.Tag] = c
	next := decideGroup(g, all, state, now)
	if next.Bad != 1 {
		t.Fatal(next)
	}
	a.Checked++
	b.Checked++
	c.Checked++
	b.DelayMS = 240
	c.DelayMS = 190
	all[a.Tag] = a
	all[b.Tag] = b
	all[c.Tag] = c
	next = decideGroup(g, all, next, now.Add(time.Second))
	if next.Current != a.Tag || next.Bad != 1 {
		t.Fatal("changing candidate retained old count", next)
	}
	a.Checked++
	c.Checked++
	all[a.Tag] = a
	all[c.Tag] = c
	next = decideGroup(g, all, next, now.Add(2*time.Second))
	if next.Current != c.Tag {
		t.Fatal("stable candidate not chosen", next)
	}
}
func TestFastestAbsoluteAndCooldown(t *testing.T) {
	g, all, state, now := policyFixture()
	g.Mode = "fastest"
	g.FastestPolicy = "absolute"
	g.ThresholdMS = 1000
	next := decideGroup(g, all, state, now)
	if next.Bad != 0 {
		t.Fatal("healthy current ignored absolute threshold")
	}
	a := all[state.Current]
	a.DelayMS = 1500
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(time.Second))
	a.Checked++
	all[a.Tag] = a
	next.Switched = now
	next = decideGroup(g, all, next, now.Add(2*time.Second))
	if next.Current != a.Tag {
		t.Fatal("cooldown ignored")
	}
	for tag, sample := range all {
		sample.Checked = now.Add(time.Minute).Unix()
		all[tag] = sample
	}
	next = decideGroup(g, all, next, now.Add(time.Minute))
	if next.Current != "auto-vpn-7-2-" {
		t.Fatal("absolute mode did not switch", next)
	}
}

func returnFixture() (BalanceGroup, map[string]GroupSample, groupDecision, time.Time) {
	g, all, state, now := policyFixture()
	g.Mode = "failover"
	g.ReturnToPreferred = true
	g.ReturnSuccesses = 2
	g.PreferredNode = "1"
	state.Current = "auto-vpn-7-2-"
	return g, all, state, now
}
func TestReturnPreferredCountsFreshSuccesses(t *testing.T) {
	g, all, state, now := returnFixture()
	next := decideGroup(g, all, state, now)
	if next.Current != state.Current || next.ReturnGood != 1 {
		t.Fatal("returned after one success", next)
	}
	next = decideGroup(g, all, next, now.Add(time.Second))
	if next.ReturnGood != 1 {
		t.Fatal("same success counted twice")
	}
	a := all["auto-vpn-7-1-"]
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(2*time.Second))
	if next.Current != a.Tag {
		t.Fatal("confirmed preferred not chosen", next)
	}
}
func TestReturnPreferredFailureResetAndCooldown(t *testing.T) {
	g, all, state, now := returnFixture()
	state.Switched = now
	next := decideGroup(g, all, state, now)
	a := all["auto-vpn-7-1-"]
	a.Alive = false
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(time.Second))
	if next.ReturnGood != 0 {
		t.Fatal("failure did not reset return")
	}
	a.Alive = true
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(2*time.Second))
	if next.ReturnGood != 1 {
		t.Fatal(next)
	}
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(3*time.Second))
	if next.Current != state.Current || next.ReturnGood != 2 {
		t.Fatal("return ignored cooldown", next)
	}
	next = decideGroup(g, all, next, now.Add(time.Minute))
	if next.Current != a.Tag {
		t.Fatal("return after cooldown missing", next)
	}
}
func TestReturnPreferredDisabledRemovedAndStale(t *testing.T) {
	g, all, state, now := returnFixture()
	g.ReturnToPreferred = false
	next := decideGroup(g, all, state, now)
	if next.Current != state.Current || next.ReturnGood != 0 {
		t.Fatal("disabled return")
	}
	g.ReturnToPreferred = true
	g.Nodes = []string{"2"}
	next = decideGroup(g, all, state, now)
	if next.Current != state.Current || next.ReturnGood != 0 {
		t.Fatal("removed preferred used")
	}
	g.Nodes = []string{"1", "2"}
	a := all["auto-vpn-7-1-"]
	a.Checked = now.Add(-time.Hour).Unix()
	all[a.Tag] = a
	next = decideGroup(g, all, state, now)
	if next.ReturnGood != 0 {
		t.Fatal("stale return accepted")
	}
}
func TestReturnPreferredFromBlockStillRequiresTwoChecks(t *testing.T) {
	g, all, state, now := returnFixture()
	g.Cooldown = 0
	state.Current = "block"
	b := all["auto-vpn-7-2-"]
	b.Alive = false
	all[b.Tag] = b
	next := decideGroup(g, all, state, now)
	if next.Current != "block" || next.ReturnGood != 1 {
		t.Fatal("unconfirmed recovery", next)
	}
	next = decideGroup(g, all, next, now.Add(time.Second))
	if next.ReturnGood != 1 {
		t.Fatal("block poll erased return progress", next)
	}
	a := all["auto-vpn-7-1-"]
	a.Checked++
	all[a.Tag] = a
	next = decideGroup(g, all, next, now.Add(2*time.Second))
	if next.Current != a.Tag {
		t.Fatal("confirmed recovery lost", next)
	}
}
func TestReturnPreferenceDoesNotChangePendingConfig(t *testing.T) {
	configDatabase(t)
	g := BalanceGroup{ID: "7", Nodes: []string{"1", "2"}, Interval: 30, Mode: "failover", ThresholdMS: 1000, Failures: 2, Cooldown: 60, ReturnToPreferred: true, ReturnSuccesses: 2}
	if e := saveGroups([]BalanceGroup{g}); e != nil {
		t.Fatal(e)
	}
	c := map[string]any{"routing": map[string]any{"balancers": []any{map[string]any{"tag": "group-7"}}}}
	for _, id := range []string{"1", "2"} {
		if e := saveSetting("group_preferred:7", id); e != nil {
			t.Fatal(e)
		}
		if preferredGroupNode(g) != id {
			t.Fatal("selection not persisted")
		}
		if groupPolicyPending(c, []BalanceGroup{g}) {
			t.Fatal("manual selection made config dirty")
		}
	}
	g.Nodes = []string{"1"}
	if preferredGroupNode(g) != "" {
		t.Fatal("removed preferred not ignored")
	}
}

func TestChangingPreferredNodeRestartsReturnConfirmation(t *testing.T) {
	g, all, state, now := returnFixture()
	state = decideGroup(g, all, state, now)
	g.Nodes = append(g.Nodes, "3")
	g.PreferredNode = "3"
	c := GroupSample{Tag: "auto-vpn-7-3-", Alive: true, DelayMS: 1500, Checked: now.Unix()}
	all[c.Tag] = c
	next := decideGroup(g, all, state, now)
	if next.Current != state.Current || next.ReturnGood != 1 {
		t.Fatal("old preference confirmation reused", next)
	}
	c.Checked++
	all[c.Tag] = c
	next = decideGroup(g, all, next, now.Add(time.Second))
	if next.Current != c.Tag {
		t.Fatal("last user choice not restored", next)
	}
}

func TestDefaultReturnRequiresTwentySuccesses(t *testing.T) {
	g, all, state, now := returnFixture()
	g.ReturnSuccesses = 0
	normalizeGroup(&g)
	if g.ReturnSuccesses != 20 {
		t.Fatal("wrong return default")
	}
	for i := 0; i < 20; i++ {
		for tag, sample := range all {
			sample.Checked = now.Unix() + int64(i)
			all[tag] = sample
		}
		state = decideGroup(g, all, state, now.Add(time.Duration(i)*time.Second))
		if i < 19 && state.Current != "auto-vpn-7-2-" {
			t.Fatal("returned before twenty successes", i)
		}
	}
	if state.Current != "auto-vpn-7-1-" {
		t.Fatal("twenty successes did not restore preferred")
	}
}
