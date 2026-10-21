package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTrafficDeltas(t *testing.T) {
	now := time.Now()
	r, v := advanceTraffic(trafficRecord{}, trafficRaw{100, 200}, "a", now)
	if v.UpBPS != 0 || v.UpBytes != 100 {
		t.Fatal(v)
	}
	r, v = advanceTraffic(r, trafficRaw{300, 600}, "a", now.Add(2*time.Second))
	if v.UpBPS != 100 || v.DownBPS != 200 || v.DownBytes != 600 {
		t.Fatal(v)
	}
	r.At = time.Time{}
	r, v = advanceTraffic(r, trafficRaw{400, 800}, "a", now.Add(3*time.Second))
	if v.UpBytes != 400 || v.UpBPS != 0 {
		t.Fatal("collector restart double counted", v)
	}
	r, v = advanceTraffic(r, trafficRaw{20, 40}, "b", now.Add(4*time.Second))
	if v.UpBytes != 420 || v.DownBytes != 840 || v.UpBPS != 0 {
		t.Fatal("epoch reset", v)
	}
	_, v = advanceTraffic(r, trafficRaw{10, 30}, "b", now.Add(5*time.Second))
	if v.UpBytes != 430 || v.UpBPS != 0 {
		t.Fatal("counter reset", v)
	}
}

func TestTrafficParsers(t *testing.T) {
	raw := []byte(`{"nftables":[{"table":{"handle":7,"comment":"fixture"}},{"set":{"name":"upload","elem":[{"elem":{"val":"192.168.1.56","counter":{"bytes":123}}}]}},{"set":{"name":"download","elem":[{"elem":{"val":"192.168.1.56","counter":{"bytes":456}}}]}}]}`)
	counts, epoch, comment, err := parseTrafficNFT(raw)
	if err != nil || epoch != "7" || comment != "fixture" || counts["192.168.1.56"] != (trafficRaw{123, 456}) {
		t.Fatal(counts, epoch, err)
	}
	raw = []byte(`{"nftables":[{"table":{"handle":9}},{"counter":{"name":"u_1029a","comment":"upload:direct","bytes":1234}},{"counter":{"name":"d_1029a","comment":"download:direct","bytes":5678}}]}`)
	counts, _, _, err = parseTrafficNFT(raw)
	if err != nil || counts["exit:direct"] != (trafficRaw{1234, 5678}) {
		t.Fatal(counts, err)
	}

}

func TestTrafficNFTFixture(t *testing.T) {
	n := GatewayNetwork{Interface: "lan0", Address: "198.18.0.1", CIDR: "198.18.0.0/24", Router: "198.18.0.254"}
	text, _, err := trafficNFT(n, "0", trafficExit{"direct", 66202}, trafficExit{"node-1", 131738})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, " drop") || strings.Contains(text, " reject") {
		t.Fatal("accounting must not change verdicts")
	}
	n.Interface = "ens18\"; delete table inet ngpanel"
	if _, _, err = trafficNFT(n, "0"); err == nil {
		t.Fatal("unsafe interface accepted")
	}
	if path := os.Getenv("NG_TRAFFIC_FIXTURE"); path != "" {
		if err = os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTrafficStaleSnapshot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NG_REVIEW", "1")
	t.Setenv("NG_STATE", dir)
	sample := TrafficSnapshot{Updated: time.Now().Add(-time.Minute), DevicesAvailable: true, OutboundsAvailable: true, Devices: map[string]TrafficValue{"192.168.1.56": {UpBytes: 500, UpBPS: 999}}, Outbounds: map[string]TrafficValue{}}
	raw, _ := json.Marshal(sample)
	os.WriteFile(dir+"/traffic.json", raw, 0600)
	got := trafficSnapshot()
	if got.ServerNow == nil || time.Since(*got.ServerNow) > time.Second {
		t.Fatal("Missing response clock", got.ServerNow)
	}
	if got.DevicesAvailable || got.Devices["192.168.1.56"].UpBPS != 0 || got.Devices["192.168.1.56"].UpBytes != 500 {
		t.Fatal(got)
	}
}

func TestTrafficRealNFTFixture(t *testing.T) {
	if path := os.Getenv("NG_TRAFFIC_REAL_NFT"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		values, _, _, err := parseTrafficNFT(raw)
		if err != nil || values["198.18.0.2"].Up < 500000 || values["198.18.0.2"].Down < 1000000 {
			t.Fatal(values, err)
		}
	}
}

func TestTrafficExitMarks(t *testing.T) {
	config := map[string]any{"outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom"}, map[string]any{"tag": "node-1", "protocol": "vless", "streamSettings": map[string]any{"sockopt": map[string]any{"domainStrategy": "UseIPv4"}}}, map[string]any{"tag": "block", "protocol": "blackhole"}}}
	assignTrafficMarks(config)
	raw, _ := json.Marshal(config)
	exits, err := parseTrafficExits(raw)
	if err != nil || len(exits) != 2 || exits[0].Mark == exits[1].Mark {
		t.Fatal(exits, err)
	}
	for _, exit := range exits {
		if exit.Mark&65535 != 666 || exit.Mark>>16 == 0 {
			t.Fatal(exit)
		}
	}
	assignTrafficMarks(config)
	again, _ := json.Marshal(config)
	if string(raw) != string(again) {
		t.Fatal("unstable marks")
	}
	config["outbounds"].([]any)[1].(map[string]any)["streamSettings"].(map[string]any)["sockopt"].(map[string]any)["mark"] = 666
	raw, _ = json.Marshal(config)
	if _, err = parseTrafficExits(raw); err == nil {
		t.Fatal("unmarked exit accepted")
	}
}

func TestTrafficResponseClockDoesNotBlessOldCollectorData(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NG_REVIEW", "1")
	t.Setenv("NG_STATE", dir)
	for _, shift := range []time.Duration{-24 * time.Hour, 24 * time.Hour} {
		wrong := time.Now().Add(shift)
		sample := TrafficSnapshot{ServerNow: &wrong, Updated: wrong, DevicesAvailable: true, OutboundsAvailable: true}
		raw, _ := json.Marshal(sample)
		os.WriteFile(dir+"/traffic.json", raw, 0600)
		got := trafficSnapshot()
		if got.DevicesAvailable || got.ServerNow == nil || time.Since(*got.ServerNow) > time.Second || time.Until(*got.ServerNow) > time.Second {
			t.Fatal(got)
		}
	}
}
