package main

import (
	"fmt"
	"strconv"
	"strings"
)

func sourceDisabled(id string) bool { return setting("source_disabled:"+id) == "1" }
func nodeDisabled(id string) bool {
	var source string
	if db.QueryRow("SELECT source_id FROM nodes WHERE id=?", id).Scan(&source) != nil {
		return false
	}
	return sourceDisabled(source)
}
func activeNodes(nodes []Node) []Node {
	out := []Node{}
	for _, n := range nodes {
		if !n.Disabled {
			out = append(out, n)
		}
	}
	return out
}
func effectiveTarget(target string) string {
	if target == "proxy" && nodeDisabled(setting("selected_node")) {
		return "block"
	}
	if strings.HasPrefix(target, "node:") && nodeDisabled(strings.TrimPrefix(target, "node:")) {
		return "block"
	}
	if strings.HasPrefix(target, "group:") {
		if g, ok := groupByID(strings.TrimPrefix(target, "group:")); ok {
			for _, id := range g.Nodes {
				if !nodeDisabled(id) {
					return target
				}
			}
			return "block"
		}
	}
	return target
}
func setSourceEnabled(id string, enabled bool) error {
	n, err := strconv.Atoi(id)
	if err != nil || n < 1 || strconv.Itoa(n) != id {
		return fmt.Errorf("Некорректная подписка")
	}
	var raw string
	if db.QueryRow("SELECT url FROM sources WHERE id=?", id).Scan(&raw) != nil || raw == "manual:" {
		return fmt.Errorf("Подписка не найдена")
	}
	value := "1"
	if enabled {
		value = "0"
	}
	return saveSetting("source_disabled:"+id, value)
}
