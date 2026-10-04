package main

import (
	"sync"
	"testing"
)

func TestGeoSearchAndValidation(t *testing.T) {
	old, oldError := geoCategories, geoError
	defer func() { geoCategories = old; geoError = oldError; geoOnce = sync.Once{} }()
	geoOnce = sync.Once{}
	geoOnce.Do(func() {})
	geoCategories = []GeoCategory{{Kind: "geosite", Code: "intel", Entries: []string{"domain:intel.com"}, Count: 1}, {Kind: "geosite", Code: "hardware", Entries: []string{"full:downloadmirror.intel.com"}, Count: 1}, {Kind: "geoip", Code: "telegram", Entries: []string{"149.154.160.0/20"}, Count: 1}}
	if len(findGeo("интел")) != 2 || len(findGeo("149.154")) != 1 {
		t.Fatal("category contents not searched")
	}
	if value, e := validateRule("domain", "geosite:intel", "proxy"); e != nil || value != "geosite:intel" {
		t.Fatalf("category rejected: %v", e)
	}
	if _, e := validateRule("ip", "geoip:telegram", "proxy"); e != nil {
		t.Fatal(e)
	}
	if _, e := validateRule("domain", "geosite:missing", "direct"); e == nil {
		t.Fatal("unknown category accepted")
	}
	if _, e := validateRule("ip", "geosite:intel", "direct"); e == nil {
		t.Fatal("wrong category type accepted")
	}
	_, next, detail := geoDetail("geosite:intel", "intel.com", "0")
	if detail == nil || len(detail.Entries) != 1 || next != 0 {
		t.Fatal("category content filter failed")
	}
	if _, _, detail = geoDetail("geosite:missing", "", "0"); detail != nil {
		t.Fatal("unknown detail accepted")
	}
	entries := make([]string, 201)
	for i := range entries {
		entries[i] = "domain:a.test"
	}
	geoCategories = append(geoCategories, GeoCategory{Kind: "geosite", Code: "large", Entries: entries, Count: 201})
	_, next, detail = geoDetail("geosite:large", "", "0")
	if next != 200 || len(detail.Entries) != 200 {
		t.Fatal("large category not paginated")
	}
	_, next, detail = geoDetail("geosite:large", "", "200")
	if next != 0 || len(detail.Entries) != 1 {
		t.Fatal("last page incorrect")
	}
}
