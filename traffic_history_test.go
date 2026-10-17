package main

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestTrafficHistoryUsesDeviceCountersOnce(t *testing.T) {
	now := time.Now()
	s := TrafficSnapshot{Updated: now, DevicesAvailable: true, Devices: map[string]TrafficValue{"pc": {UpBPS: 10, DownBPS: 200, UpBytes: 100, DownBytes: 2000}, "phone": {UpBPS: 5, DownBPS: 20, UpBytes: 50, DownBytes: 100}}, Outbounds: map[string]TrafficValue{"vpn": {DownBPS: 20000}}}
	history := appendTrafficHistory(nil, s)
	if len(history) != 1 || history[0].Up != 15 || history[0].Down != 220 || history[0].Sent != 150 || history[0].Received != 2100 {
		t.Fatal(history)
	}
	if len(appendTrafficHistory(history, s)) != 1 {
		t.Fatal("duplicate snapshot")
	}
	s.Updated = now.Add(time.Second)
	s.DevicesAvailable = false
	if len(appendTrafficHistory(history, s)) != 1 {
		t.Fatal("invented zero during failure")
	}
}
func TestTrafficHistorySurvivesAbsenceAndSerialization(t *testing.T) {
	now := time.Now()
	var history []TrafficHistoryPoint
	for i := 0; i < 200; i++ {
		history = appendTrafficHistory(history, TrafficSnapshot{Updated: now.Add(time.Duration(i) * time.Second), DevicesAvailable: true, Devices: map[string]TrafficValue{"pc": {DownBPS: float64(i + 1)}}})
	}
	if len(history) != 91 {
		t.Fatal(len(history))
	}
	raw, _ := json.Marshal(history)
	restored := restoreTrafficHistory(string(raw), now.Add(200*time.Second))
	if len(restored) < 89 || restored[len(restored)-1].Down != 200 {
		t.Fatal("history missing", len(restored))
	}
	if len(restoreTrafficHistory(string(raw), now.Add(300*time.Second))) != 0 {
		t.Fatal("expired history retained")
	}
}
func TestTrafficHistoryRejectsInvalidValues(t *testing.T) {
	now := time.Now()
	points := []TrafficHistoryPoint{{Time: now.UnixMilli(), Up: 1, Down: 2}, {Time: now.UnixMilli(), Up: 3}, {Time: now.UnixMilli() - 91000}, {Time: now.UnixMilli() + 10000}, {Time: now.UnixMilli() + 1, Up: math.NaN()}, {Time: now.UnixMilli() + 2, Down: -1}}
	got := retainTrafficHistory(points, now)
	if len(got) != 1 || got[0].Up != 1 {
		t.Fatal(got)
	}
	for _, raw := range []string{"broken", `[{"time":1,"sent":-1}]`} {
		if len(restoreTrafficHistory(raw, now)) != 0 {
			t.Fatal("invalid history accepted")
		}
	}
}
