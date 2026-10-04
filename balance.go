package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

type BalanceGroup struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Nodes    []string            `json:"nodes"`
	Interval int                 `json:"interval"`
	Members  []BalanceMemberView `json:"-"`
}

type BalanceSettings struct {
	Enabled  bool     `json:"enabled"`
	Nodes    []string `json:"nodes"`
	Interval int      `json:"interval"`
}

type BalanceStatus struct {
	Message, Tag, Name string
}

type BalanceMemberView struct {
	Node             Node
	Source           string
	Selected, Active bool
}

func balanceMemberViews(b BalanceSettings, selected string, nodes []Node, sources []Source, active string) []BalanceMemberView {
	byID := map[string]Node{}
	bySource := map[int]string{}
	for _, n := range nodes {
		byID[strconv.Itoa(n.ID)] = n
	}
	for _, s := range sources {
		bySource[s.ID] = s.Name
	}
	seen := map[string]bool{}
	var out []BalanceMemberView
	for _, id := range b.Nodes {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		n, ok := byID[id]
		if !ok {
			continue
		}
		out = append(out, BalanceMemberView{Node: n, Source: bySource[n.SourceID], Selected: id == selected, Active: active == "auto-vpn-"+id+"-"})
	}
	return out
}

func balanceSettings() BalanceSettings {
	var b BalanceSettings
	json.Unmarshal([]byte(setting("balance_settings")), &b)
	if b.Interval == 0 {
		b.Interval = 30
	}
	return b
}

func balanceMembers(b BalanceSettings, selected string) ([]string, error) {
	if b.Interval < 10 || b.Interval > 600 {
		return nil, fmt.Errorf("Интервал проверки: от 10 до 600 секунд")
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range b.Nodes {
		if id == "" {
			continue
		}
		n, e := strconv.Atoi(id)
		if e != nil || n < 1 || strconv.Itoa(n) != id {
			return nil, fmt.Errorf("Некорректное подключение в группе")
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	if len(ids) < 2 || len(ids) > 8 {
		return nil, fmt.Errorf("Для автовыбора выберите от 2 до 8 подключений, без автоматического добавления выбранного VPN")
	}
	return ids, nil
}

func balanceGroups() []BalanceGroup {
	var groups []BalanceGroup
	raw := setting("balance_groups")
	if raw != "" {
		json.Unmarshal([]byte(raw), &groups)
		return groups
	}
	// Keep the previous pool as an independent, unassigned group.
	old := balanceSettings()
	if len(old.Nodes) > 0 {
		ids := append([]string{}, old.Nodes...)
		selected := setting("selected_node")
		if selected != "" && !slices.Contains(ids, selected) {
			ids = append(ids, selected)
		}
		groups = append(groups, BalanceGroup{ID: "1", Name: "Группа автовыбора VPN", Nodes: ids, Interval: old.Interval})
	}
	return groups
}
func groupByID(id string) (BalanceGroup, bool) {
	for _, g := range balanceGroups() {
		if g.ID == id {
			return g, true
		}
	}
	return BalanceGroup{}, false
}
func saveGroups(groups []BalanceGroup) error {
	raw, _ := json.Marshal(groups)
	return saveSetting("balance_groups", string(raw))
}
func saveBalance(r *http.Request) error {
	groups := balanceGroups()
	id := r.FormValue("group_id")
	name := strings.TrimSpace(r.FormValue("name"))
	interval, _ := strconv.Atoi(r.FormValue("interval"))
	if name == "" || len(name) > 200 {
		return fmt.Errorf("Название группы: от 1 до 200 символов")
	}
	ids, e := balanceMembers(BalanceSettings{Nodes: r.Form["balance_node"], Interval: interval}, "")
	if e != nil {
		return e
	}
	for _, node := range ids {
		if e = validateRuleNode("node:" + node); e != nil {
			return e
		}
	}
	if id == "" {
		id = strconv.FormatInt(time.Now().UnixNano(), 10)
		groups = append(groups, BalanceGroup{ID: id})
	}
	found := false
	for i := range groups {
		if groups[i].ID == id {
			groups[i] = BalanceGroup{ID: id, Name: name, Nodes: ids, Interval: interval}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("Группа отсутствует")
	}
	if len(groups) > 16 {
		return fmt.Errorf("Не более 16 групп")
	}
	return saveGroups(groups)
}
func deleteBalance(id string) error {
	if setting("default_route") == "group:"+id {
		return fmt.Errorf("Группа используется маршрутом по умолчанию")
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM rules WHERE target=?", "group:"+id).Scan(&n)
	if n > 0 {
		return fmt.Errorf("Группа используется правилами; сначала смените их выход")
	}
	groups := balanceGroups()
	filtered := []BalanceGroup{}
	found := false
	for _, g := range groups {
		if g.ID == id {
			found = true
		} else {
			filtered = append(filtered, g)
		}
	}
	if !found {
		return fmt.Errorf("Группа отсутствует")
	}
	return saveGroups(filtered)
}
func addGroups(config map[string]any) error {
	routing := config["routing"].(map[string]any)
	used := map[string]bool{}
	for _, r := range routing["rules"].([]any) {
		entry := r.(map[string]any)
		tag, _ := entry["outboundTag"].(string)
		if strings.HasPrefix(tag, "group:") {
			used[strings.TrimPrefix(tag, "group:")] = true
			delete(entry, "outboundTag")
			entry["balancerTag"] = "group-" + strings.TrimPrefix(tag, "group:")
		}
	}
	if len(used) == 0 {
		return nil
	}
	out := config["outbounds"].([]any)
	balancers := []any{}
	hosts := []string{}
	interval := 600
	// Iterate in saved order so configuration hashes stay stable.
	for _, g := range balanceGroups() {
		if !used[g.ID] {
			continue
		}
		delete(used, g.ID)
		ids, e := balanceMembers(BalanceSettings{Nodes: g.Nodes, Interval: g.Interval}, "")
		if e != nil {
			return e
		}
		if g.Interval < interval {
			interval = g.Interval
		}
		prefix := "auto-vpn-" + g.ID + "-"
		for _, id := range ids {
			var raw string
			if db.QueryRow("SELECT uri FROM nodes WHERE id=?", id).Scan(&raw) != nil {
				return fmt.Errorf("Участник группы %s отсутствует", g.Name)
			}
			o, e := nodeOutbound(raw)
			if e != nil {
				return e
			}
			o["tag"] = prefix + id + "-"
			out = append(out, o)
			u, _ := url.Parse(raw)
			if u != nil {
				hosts = append(hosts, "full:"+u.Hostname())
			}
		}
		balancers = append(balancers, map[string]any{"tag": "group-" + g.ID, "selector": []string{prefix}, "fallbackTag": "block", "strategy": map[string]any{"type": "leastPing"}})
	}
	if len(used) > 0 {
		return fmt.Errorf("Группа маршрута отсутствует")
	}
	config["outbounds"] = out
	routing["balancers"] = balancers
	config["observatory"] = map[string]any{"subjectSelector": []string{"auto-vpn-"}, "probeUrl": "https://www.google.com/generate_204", "probeInterval": fmt.Sprintf("%ds", interval), "enableConcurrency": true}
	config["api"] = map[string]any{"tag": "balance-api", "listen": "127.0.0.1:10085", "services": []string{"RoutingService"}}
	dns := config["dns"].(map[string]any)
	servers := dns["servers"].([]any)
	dns["servers"] = append([]any{map[string]any{"address": setting("dns_direct"), "domains": hosts, "skipFallback": true, "finalQuery": true, "tag": "dns-bootstrap"}}, servers...)
	return nil
}

func readGroupStatus(id string) BalanceStatus {
	if !readRuntime().Balance {
		return BalanceStatus{Message: "Автовыбор не включён в применённой конфигурации"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, e := exec.CommandContext(ctx, "/usr/local/bin/xray", "api", "bi", "-json", "-s=127.0.0.1:10085", "-t=1", "group-"+id).Output()
	if e != nil {
		return BalanceStatus{Message: "Группа не используется в применённых маршрутах или Xray недоступен"}
	}
	var info struct {
		Balancer struct {
			PrincipleTarget struct {
				Tag []string `json:"tag"`
			} `json:"principleTarget"`
		} `json:"balancer"`
	}
	if json.Unmarshal(raw, &info) != nil {
		return BalanceStatus{Message: "Не удалось прочитать состояние балансера"}
	}
	if len(info.Balancer.PrincipleTarget.Tag) == 0 || info.Balancer.PrincipleTarget.Tag[0] == "" {
		return BalanceStatus{Message: "Нет доступного выхода или первые проверки ещё не завершены; новые VPN-соединения блокируются"}
	}
	tag := info.Balancer.PrincipleTarget.Tag[0]
	nodeID := strings.TrimSuffix(strings.TrimPrefix(tag, "auto-vpn-"+id+"-"), "-")
	var name string
	db.QueryRow("SELECT name FROM nodes WHERE id=?", nodeID).Scan(&name)
	if name == "" {
		name = tag
	}
	return BalanceStatus{Tag: tag, Name: name, Message: "Выход для новых соединений: " + name}
}
