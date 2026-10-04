package main

import (
	"fmt"
	"strconv"
	"strings"
)

func validTarget(target string) bool {
	if target == "direct" || target == "proxy" || target == "block" {
		return true
	}
	if !strings.HasPrefix(target, "node:") {
		return false
	}
	id, e := strconv.Atoi(strings.TrimPrefix(target, "node:"))
	return e == nil && id > 0 && target == fmt.Sprintf("node:%d", id)
}
func targetTag(target string) string {
	if strings.HasPrefix(target, "node:") {
		return "node-" + strings.TrimPrefix(target, "node:")
	}
	return target
}
func targetLabel(target string) string {
	switch target {
	case "direct":
		return "Провайдер"
	case "proxy":
		return "Выбранный VPN"
	case "block":
		return "Блокировать"
	}
	var name string
	if db.QueryRow("SELECT name FROM nodes WHERE id=?", strings.TrimPrefix(target, "node:")).Scan(&name) != nil {
		return "VPN: подключение отсутствует"
	}
	return "VPN: " + name
}
func validateRuleNode(target string) error {
	if !validTarget(target) {
		return fmt.Errorf("Некорректный выход правила")
	}
	if !strings.HasPrefix(target, "node:") {
		return nil
	}
	var raw string
	if db.QueryRow("SELECT uri FROM nodes WHERE id=?", strings.TrimPrefix(target, "node:")).Scan(&raw) != nil {
		return fmt.Errorf("Подключение отсутствует")
	}
	out, e := nodeOutbound(raw)
	if e != nil {
		return e
	}
	return validateXrayOutbound(out)
}
func referencedNodes() map[string]bool {
	out := map[string]bool{}
	for _, id := range balanceSettings().Nodes {
		out[id] = true
	}
	rows, e := db.Query("SELECT target FROM rules WHERE target LIKE 'node:%'")
	if e != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var target string
		if rows.Scan(&target) == nil {
			out[strings.TrimPrefix(target, "node:")] = true
		}
	}
	return out
}

func sourceProtected(source string) bool {
	refs := referencedNodes()
	refs[setting("selected_node")] = true
	for id := range refs {
		var parent string
		if db.QueryRow("SELECT source_id FROM nodes WHERE id=?", id).Scan(&parent) == nil && parent == source {
			return true
		}
	}
	return false
}
