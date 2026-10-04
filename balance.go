package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

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
	for _, id := range append([]string{selected}, b.Nodes...) {
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
	for _, id := range append([]string{selected}, b.Nodes...) {
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
		return nil, fmt.Errorf("Для автовыбора выберите от 2 до 8 подключений, включая выбранный VPN")
	}
	return ids, nil
}

func saveBalance(r *http.Request) error {
	b := BalanceSettings{Enabled: r.FormValue("enabled") == "1", Nodes: r.Form["balance_node"]}
	b.Interval, _ = strconv.Atoi(r.FormValue("interval"))
	if len(b.Nodes) > 8 {
		return fmt.Errorf("Не более 8 участников группы")
	}
	clean := []string{}
	seen := map[string]bool{}
	for _, id := range b.Nodes {
		if e := validateRuleNode("node:" + id); e != nil {
			return e
		}
		if !seen[id] {
			clean = append(clean, id)
			seen[id] = true
		}
	}
	b.Nodes = clean

	if b.Interval < 10 || b.Interval > 600 {
		return fmt.Errorf("Интервал проверки: от 10 до 600 секунд")
	}
	if b.Enabled {
		ids, e := balanceMembers(b, setting("selected_node"))
		if e != nil {
			return e
		}
		for _, id := range ids {
			if e = validateRuleNode("node:" + id); e != nil {
				return e
			}
		}
	}
	raw, _ := json.Marshal(b)
	return saveSetting("balance_settings", string(raw))
}

func addBalance(config map[string]any, b BalanceSettings) error {
	ids, e := balanceMembers(b, setting("selected_node"))
	if e != nil {
		return e
	}
	out := config["outbounds"].([]any)
	// The ordinary selected VPN is replaced by a group; fixed node rules remain fixed.
	filtered := []any{}
	for _, o := range out {
		if o.(map[string]any)["tag"] != "proxy" {
			filtered = append(filtered, o)
		}
	}
	hosts := []string{}
	for _, id := range ids {
		var raw string
		if db.QueryRow("SELECT uri FROM nodes WHERE id=?", id).Scan(&raw) != nil {
			return fmt.Errorf("Подключение группы #%s отсутствует", id)
		}
		o, e := nodeOutbound(raw)
		if e != nil {
			return e
		}
		o["tag"] = "auto-vpn-" + id + "-"
		filtered = append(filtered, o)
		u, _ := url.Parse(raw)
		if u != nil && u.Hostname() != "" {
			hosts = append(hosts, "full:"+u.Hostname())
		}
	}
	config["outbounds"] = filtered
	routing := config["routing"].(map[string]any)
	for _, r := range routing["rules"].([]any) {
		entry := r.(map[string]any)
		if entry["outboundTag"] == "proxy" {
			delete(entry, "outboundTag")
			entry["balancerTag"] = "auto-vpn"
		}
	}
	routing["balancers"] = []any{map[string]any{"tag": "auto-vpn", "selector": []string{"auto-vpn-"}, "fallbackTag": "block", "strategy": map[string]any{"type": "leastPing"}}}
	config["observatory"] = map[string]any{"subjectSelector": []string{"auto-vpn-"}, "probeUrl": "https://www.google.com/generate_204", "probeInterval": fmt.Sprintf("%ds", b.Interval), "enableConcurrency": true}
	config["api"] = map[string]any{"tag": "balance-api", "listen": "127.0.0.1:10085", "services": []string{"RoutingService"}}
	// All group server hostnames bootstrap directly, independent of VPN DNS.
	dns := config["dns"].(map[string]any)
	servers := dns["servers"].([]any)
	servers = append([]any{map[string]any{"address": setting("dns_direct"), "domains": hosts, "skipFallback": true, "finalQuery": true, "tag": "dns-bootstrap"}}, servers...)
	dns["servers"] = servers
	return nil
}

func readBalanceStatus() BalanceStatus {
	if !readRuntime().Balance {
		return BalanceStatus{Message: "Автовыбор не включён в применённой конфигурации"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, e := exec.CommandContext(ctx, "/usr/local/bin/xray", "api", "bi", "-json", "-s=127.0.0.1:10085", "-t=1", "auto-vpn").Output()
	if e != nil {
		return BalanceStatus{Message: "Автовыбор пока не применён или Xray недоступен"}
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
	id := strings.TrimSuffix(strings.TrimPrefix(tag, "auto-vpn-"), "-")
	var name string
	db.QueryRow("SELECT name FROM nodes WHERE id=?", id).Scan(&name)
	if name == "" {
		name = tag
	}
	return BalanceStatus{Tag: tag, Name: name, Message: "Выход для новых соединений: " + name}
}
