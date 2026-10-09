package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type GeoCategory struct {
	Kind, Code string
	Entries    []string
	Count      int
	Example    string
}

var geoOnce sync.Once
var geoCategories []GeoCategory
var geoError string

func loadGeo() {
	geoOnce.Do(func() {
		b, e := os.ReadFile("/usr/local/share/ngpanel-geodata/index.json")
		if e == nil {
			e = json.Unmarshal(b, &geoCategories)
		}
		if e != nil {
			geoError = fmt.Sprintf("Не удалось прочитать индекс geo-баз: %v", e)
		}
	})
}
func findGeo(query string) []GeoCategory {
	loadGeo()
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "интел" {
		q = "intel"
	}
	if q == "" {
		return nil
	}
	if len(q) > 128 {
		return nil
	}
	var out []GeoCategory
	for _, c := range geoCategories {
		match := strings.Contains(c.Code, q)
		examples := []string{}
		for _, entry := range c.Entries {
			if strings.Contains(strings.ToLower(entry), q) {
				match = true
				if len(examples) < 3 {
					examples = append(examples, entry)
				}
			}
		}
		if match {
			c.Entries = nil
			c.Example = strings.Join(examples, "\n")
			out = append(out, c)
		}
	}
	rank := func(c GeoCategory) int {
		if c.Code == q {
			return 0
		}
		if strings.Contains(c.Code, q) {
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := rank(out[i]), rank(out[j])
		if a != b {
			return a < b
		}
		return out[i].Code < out[j].Code
	})
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}
func validGeo(value, kind string) error {
	loadGeo()
	if geoError != "" {
		return fmt.Errorf("%s", geoError)
	}
	for _, c := range geoCategories {
		if c.Kind == kind && value == kind+":"+c.Code && c.Count > 0 {
			return nil
		}
	}
	return fmt.Errorf("Категория %s отсутствует в установленной базе", value)
}

func geoDetail(token, filter, offset string) (int, int, *GeoCategory) {
	loadGeo()
	start, _ := strconv.Atoi(offset)
	if start < 0 {
		start = 0
	}
	for _, c := range geoCategories {
		if token != c.Kind+":"+c.Code {
			continue
		}
		entries := []string{}
		for _, entry := range c.Entries {
			if strings.Contains(strings.ToLower(entry), strings.ToLower(filter)) {
				entries = append(entries, entry)
			}
		}
		if start > len(entries) {
			start = 0
		}
		end := start + 200
		if end > len(entries) {
			end = len(entries)
		}
		c.Example = fmt.Sprintf("Всего: %d; после фильтра: %d; показано: %d–%d", c.Count, len(entries), start, end)
		c.Entries = entries[start:end]
		next := 0
		if end < len(entries) {
			next = end
		}
		return start, next, &c
	}
	return 0, 0, nil
}
