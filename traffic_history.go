package main

import (
	"encoding/json"
	"math"
	"sort"
	"time"
)

type TrafficHistoryPoint struct {
	Time     int64   `json:"time"`
	Up       float64 `json:"up"`
	Down     float64 `json:"down"`
	Sent     uint64  `json:"sent"`
	Received uint64  `json:"received"`
}

func retainTrafficHistory(points []TrafficHistoryPoint, now time.Time) []TrafficHistoryPoint {
	out := make([]TrafficHistoryPoint, 0, 96)
	end := now.UnixMilli()
	for _, p := range points {
		if p.Time >= end-90000 && p.Time <= end+5000 && p.Up >= 0 && p.Down >= 0 && !math.IsNaN(p.Up) && !math.IsNaN(p.Down) && !math.IsInf(p.Up, 0) && !math.IsInf(p.Down, 0) {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	unique := out[:0]
	for _, p := range out {
		if len(unique) == 0 || unique[len(unique)-1].Time != p.Time {
			unique = append(unique, p)
		}
	}
	if len(unique) > 96 {
		unique = unique[len(unique)-96:]
	}
	return unique
}
func restoreTrafficHistory(raw string, now time.Time) []TrafficHistoryPoint {
	var points []TrafficHistoryPoint
	if len(raw) > 128000 || json.Unmarshal([]byte(raw), &points) != nil {
		return nil
	}
	return retainTrafficHistory(points, now)
}
func appendTrafficHistory(points []TrafficHistoryPoint, s TrafficSnapshot) []TrafficHistoryPoint {
	points = retainTrafficHistory(points, s.Updated)
	if !s.DevicesAvailable || s.Updated.IsZero() || len(points) > 0 && points[len(points)-1].Time >= s.Updated.UnixMilli() {
		return points
	}
	p := TrafficHistoryPoint{Time: s.Updated.UnixMilli()}
	for _, v := range s.Devices {
		if v.UpBPS >= 0 && !math.IsNaN(v.UpBPS) && !math.IsInf(v.UpBPS, 0) {
			p.Up += v.UpBPS
		}
		if v.DownBPS >= 0 && !math.IsNaN(v.DownBPS) && !math.IsInf(v.DownBPS, 0) {
			p.Down += v.DownBPS
		}
		p.Sent += v.UpBytes
		p.Received += v.DownBytes
	}
	return retainTrafficHistory(append(points, p), s.Updated)
}
