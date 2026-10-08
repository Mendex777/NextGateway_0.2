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
	if target == "proxy" && !nodeAvailable(setting("selected_node")) {
		return unavailableTarget()
	}
	if strings.HasPrefix(target, "node:") && !nodeAvailable(strings.TrimPrefix(target, "node:")) {
		return unavailableTarget()
	}
	if strings.HasPrefix(target, "group:") {
		if g, ok := groupByID(strings.TrimPrefix(target, "group:")); ok {
			if _, err := balanceMembers(BalanceSettings{Nodes: g.Nodes, Interval: g.Interval}, ""); err != nil {
				return target
			}
			for _, id := range g.Nodes {
				if nodeAvailable(id) {
					return target
				}
			}
			return unavailableTarget()
		}
	}
	return target
}
func unavailableTarget() string {
	if setting("vpn_unavailable") == "direct" {
		return "direct"
	}
	return "block"
}
func nodeAvailable(id string) bool {
	var exists int
	if db.QueryRow("SELECT id FROM nodes WHERE id=?", id).Scan(&exists) != nil {
		return false
	}
	return !nodeDisabled(id)
}
func vpnAvailabilityWarning() string {
	count := 0
	for _, r := range allRules() {
		if !r.Disabled && effectiveTarget(r.Target) != r.Target {
			count++
		}
	}
	missingDefault := effectiveTarget(setting("default_route")) != setting("default_route")
	missingDNS := setting("dns_mode") == "proxy" && !nodeAvailable(setting("selected_node"))
	if count == 0 && !missingDefault && !missingDNS {
		return ""
	}
	behavior := "Трафик блокируется"
	if unavailableTarget() == "direct" {
		behavior = "Трафик идёт напрямую через провайдера"
	}
	return fmt.Sprintf("VPN-выход недоступен. Правил без доступного выхода: %d. %s после применения конфигурации.", count, behavior)
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
