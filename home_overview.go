package main

import (
	"encoding/json"
	"strings"
)

// HomeOverview describes applied routing, never the pending editor contents.
type HomeOverview struct {
	Available bool
	Default   string
	RuleCount int
	Exits     []HomeExit
}
type HomeMember struct{ Tag, Name string }
type HomeExit struct {
	Members            []HomeMember
	Tag, Name, GroupID string
	Rules              int
}

func summarizeHome(raw []byte, runtime Runtime, names map[string]string) HomeOverview {
	var config struct {
		Routing struct {
			Rules []struct{ RuleTag, OutboundTag, BalancerTag string }
		}
	}
	result := HomeOverview{}
	if json.Unmarshal(raw, &config) != nil || len(config.Routing.Rules) == 0 {
		return result
	}
	positions := map[string]int{}
	for _, rule := range config.Routing.Rules {
		user := strings.HasPrefix(rule.RuleTag, "ngpanel-rule:")
		fallback := connectionRuleLabel(rule.RuleTag) == "Маршрут по умолчанию"
		if !user && !fallback {
			continue
		}
		tag := rule.OutboundTag
		if rule.BalancerTag != "" {
			tag = rule.BalancerTag
		}
		name, groupID := tag, ""
		switch tag {
		case "direct":
			name = "Direct — провайдер"
		case "block":
			name = "Блокировка"
		case "proxy":
			name = "Выбранный VPN"
		}
		if strings.HasPrefix(tag, "node-") {
			if n := names[strings.TrimPrefix(tag, "node-")]; n != "" {
				name = n
			}
		}
		for _, g := range runtime.Groups {
			if tag == "group-"+g.ID {
				name = g.Name
				groupID = g.ID
			}
		}
		if fallback {
			result.Default = name
			result.Available = true
		}
		if user {
			result.RuleCount++
		}
		if i, ok := positions[tag]; ok {
			if user {
				result.Exits[i].Rules++
			}
		} else {
			positions[tag] = len(result.Exits)
			n := 0
			if user {
				n = 1
			}
			exit := HomeExit{Tag: tag, Name: name, GroupID: groupID, Rules: n}
			for _, g := range runtime.Groups {
				if g.ID == groupID {
					for _, id := range g.Nodes {
						label := names[id]
						if label == "" {
							label = "Узел " + id
						}
						exit.Members = append(exit.Members, HomeMember{Tag: "auto-vpn-" + g.ID + "-" + id + "-", Name: label})
					}
				}
			}
			result.Exits = append(result.Exits, exit)
		}
	}
	return result
}

func homeOverview(runtime Runtime) HomeOverview {
	raw, err := dashboardConfig()
	if err != nil {
		return HomeOverview{}
	}
	names := map[string]string{}
	rows, err := db.Query("SELECT id,name FROM nodes")
	if err == nil {
		for rows.Next() {
			var id, name string
			if rows.Scan(&id, &name) == nil {
				names[id] = name
			}
		}
		rows.Close()
	}
	return summarizeHome(raw, runtime, names)
}
