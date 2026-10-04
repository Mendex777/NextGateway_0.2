package main

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

func validateSource(raw string) (string, error) {
	var out []string
	for _, s := range strings.FieldsFunc(raw, func(c rune) bool { return c == ',' || c == '\n' }) {
		s = strings.TrimSpace(s)
		if strings.Contains(s, ":") {
			return "", fmt.Errorf("IP устройства: только IPv4 или IPv4/CIDR")
		}
		if !validIPv4(s) {
			ip, network, e := net.ParseCIDR(s)
			if e != nil || ip.To4() == nil {
				return "", fmt.Errorf("IP устройства: только IPv4 или IPv4/CIDR")
			}
			s = network.String()
		}
		out = append(out, s)
	}
	if len(out) > 64 {
		return "", fmt.Errorf("Не более 64 IP устройств в правиле")
	}
	return strings.Join(out, "\n"), nil
}
func ipMatches(value, address string) bool {
	ip := net.ParseIP(address)
	if ip == nil {
		return false
	}
	if _, network, e := net.ParseCIDR(value); e == nil {
		return network.Contains(ip)
	}
	return ip.Equal(net.ParseIP(value))
}
func domainMatches(value, domain string) bool {
	domain = strings.TrimSuffix(strings.ToLower(domain), ".")
	prefix, host, ok := strings.Cut(value, ":")
	if !ok {
		prefix = "plain"
		host = value
	}
	switch prefix {
	case "domain":
		host = strings.ToLower(host)
		return domain == host || strings.HasSuffix(domain, "."+host)
	case "full":
		return domain == strings.ToLower(host)
	case "plain":
		return strings.Contains(domain, strings.ToLower(host))
	case "regexp":
		r, e := regexp.Compile(host)
		return e == nil && r.MatchString(domain)
	}
	return false
}

// Three states avoid claiming a match when a required address was omitted.
func destinationMatch(rule Rule, domain, address string) int {
	if rule.Kind == "device" {
		return 1
	}
	input := domain
	if rule.Kind == "ip" {
		input = address
	}
	if input == "" {
		return -1
	}
	loadGeo()
	for _, value := range strings.FieldsFunc(rule.Value, func(c rune) bool { return c == ',' || c == '\n' }) {
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "geosite:") || strings.HasPrefix(value, "geoip:") {
			for _, category := range geoCategories {
				if value != category.Kind+":"+category.Code {
					continue
				}
				for _, entry := range category.Entries {
					if rule.Kind == "domain" && domainMatches(entry, input) || rule.Kind == "ip" && ipMatches(entry, input) {
						return 1
					}
				}
			}
		} else if rule.Kind == "domain" && domainMatches(value, input) || rule.Kind == "ip" && ipMatches(value, input) {
			return 1
		}
	}
	return 0
}

type RouteCheck struct{ Domain, Address, Source, Result, Target, TargetLabel, Note, Reason, DNSReason, DNSTarget, DNSLabel, DNSServers, Warning string }

func checkRoute(domain, address, source string, rules []Rule, mode string) RouteCheck {
	c := RouteCheck{Domain: strings.TrimSpace(strings.ToLower(domain)), Address: strings.TrimSpace(address), Source: strings.TrimSpace(source)}
	if c.Domain != "" {
		c.Domain = strings.TrimSuffix(c.Domain, ".")
		if len(c.Domain) > 253 || strings.ContainsAny(c.Domain, " /\\:\t\r\n") {
			c.Result = "Введите домен без протокола, порта и пути"
			return c
		}
	}
	for _, ip := range []string{c.Address, c.Source} {
		if ip != "" && !validIPv4(ip) {
			c.Result = "Адрес назначения и IP устройства должны быть IPv4"
			return c
		}
	}
	if c.Domain == "" && c.Address == "" {
		c.Result = "Укажите домен или IPv4 назначения"
		return c
	}
	c.Note = "Проверка сохранённых правил для TCP/UDP. Домен предполагается распознанным Xray; DNS автоматически не запрашивается."
	if c.Address == "" {
		c.Note += " Адрес назначения не задан: исключение локальной сети не проверено; расчёт предполагает публичный IPv4."
	}
	for _, local := range []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		if ipMatches(local, c.Address) {
			c.Reason = "IPv4 назначения входит в исключение локальной сети, которое проверяется раньше пользовательских правил."
			c.Result = "Исключение локальной сети"
			c.Target = "direct"
			return c
		}
	}
	var unchecked []string
	for _, rule := range rules {
		if rule.Disabled {
			continue
		}
		dest := destinationMatch(rule, c.Domain, c.Address)
		if dest == 0 {
			continue
		}
		src := 1
		if rule.Source != "" {
			src = 0
			if c.Source == "" {
				src = -1
			} else {
				for _, item := range strings.Split(rule.Source, "\n") {
					if ipMatches(item, c.Source) {
						src = 1
						break
					}
				}
			}
		}
		if src == 0 {
			continue
		}
		if dest < 0 || src < 0 {
			missing := "IPv4 назначения"
			if src < 0 {
				missing = "IP устройства"
			} else if rule.Kind != "ip" {
				missing = "домен назначения"
			}
			unchecked = append(unchecked, fmt.Sprintf("#%d «%s» (%s)", rule.ID, rule.Name, missing))
			continue
		}

		c.Reason = fmt.Sprintf("Первое подходящее включённое правило #%d: тип %s", rule.ID, rule.Kind)
		if rule.Source != "" {
			c.Reason += "; IP устройства соответствует источнику правила"
		}
		c.Result = fmt.Sprintf("Правило «%s»", rule.Name)
		c.Target = rule.Target
		markUnchecked(&c, unchecked)
		return c
	}
	c.Reason = "Ни одно включённое правило выше не совпало с указанным назначением и устройством."
	c.Result = "Совпавших правил нет — маршрут по умолчанию"
	c.Target = mode
	markUnchecked(&c, unchecked)
	return c
}

// Inspect generated DNS configuration so bootstrap exceptions and reserves match the core.
func explainDNS(c *RouteCheck) {
	if c.Target == "" {
		return
	}
	c.TargetLabel = targetLabel(c.Target)
	if c.Target == "proxy" {
		c.TargetLabel += ": " + targetLabel("node:"+setting("selected_node"))
	}
	if c.Domain == "" {
		c.DNSReason = "Укажите домен для расчёта DNS."
		return
	}
	config, e := buildConfig()
	if e != nil {
		c.DNSReason = "Не удалось построить сохранённую конфигурацию: " + e.Error()
		return
	}
	dns := config["dns"].(map[string]any)
	entries := dns["servers"].([]any)
	chosen := []map[string]any{}
	for _, item := range entries {
		server := item.(map[string]any)
		domains, _ := server["domains"].([]string)
		if len(domains) == 0 {
			continue
		}
		if destinationMatch(Rule{Kind: "domain", Value: strings.Join(domains, "\n")}, c.Domain, "") != 1 {
			continue
		}
		chosen = append(chosen, server)
		if final, _ := server["finalQuery"].(bool); final {
			break
		}
	}
	if len(chosen) == 0 {
		for _, item := range entries {
			server := item.(map[string]any)
			if skip, _ := server["skipFallback"].(bool); !skip {
				chosen = append(chosen, server)
			}
		}
	}
	if len(chosen) == 0 {
		c.DNSReason = "Нет подходящих DNS-серверов."
		return
	}
	tag := chosen[0]["tag"].(string)
	for _, item := range config["routing"].(map[string]any)["rules"].([]any) {
		rule := item.(map[string]any)
		tags, _ := rule["inboundTag"].([]string)
		for _, t := range tags {
			if t == tag {
				c.DNSTarget, _ = rule["outboundTag"].(string)
				if bal, ok := rule["balancerTag"].(string); ok {
					c.DNSTarget = "group:" + strings.TrimPrefix(bal, "group-")
				}
			}
		}
	}
	if strings.HasPrefix(c.DNSTarget, "node-") {
		c.DNSTarget = "node:" + strings.TrimPrefix(c.DNSTarget, "node-")
	}
	c.DNSLabel = targetLabel(c.DNSTarget)
	if c.DNSTarget == "proxy" {
		c.DNSLabel += ": " + targetLabel("node:"+setting("selected_node"))
	}
	for _, server := range chosen {
		if c.DNSServers != "" {
			c.DNSServers += "\n"
		}
		c.DNSServers += server["address"].(string)
	}
	switch {
	case tag == "dns-bootstrap":
		c.DNSReason = "Прямое разрешение адреса VPN или DoH сервера (Bootstrap)."
	case strings.HasPrefix(tag, "dns-rule-"):
		c.DNSReason = "Первое подходящее общее доменное правило #" + strings.TrimPrefix(tag, "dns-rule-") + "; резервные серверы используют тот же выход."
	default:
		if setting("dns_mode") == "rules" {
			c.DNSReason = "Общего доменного правила нет: DNS следует маршруту по умолчанию."
		} else {
			c.DNSReason = "Общий режим DNS; правила устройств не изменяют этот режим."
		}
	}
	if c.Target != "block" && c.DNSTarget != c.Target {
		c.Warning += " Выход трафика и DNS различается. DNS-кеш общий: правила отдельных устройств и IP-правила не определяют DNS. Это расчёт настроек, а не наблюдение реального запроса."
	}
	c.Note += " DNS рассчитан для A/AAAA по сохранённой конфигурации; TXT/MX пересылаются напрямую на Bootstrap DNS. Собственный DoH приложения может обходить DNS ВМ."
}

func markUnchecked(c *RouteCheck, rules []string) {
	if len(rules) == 0 {
		return
	}
	c.Result = "Предварительно: " + c.Result
	c.Warning = "Ранее расположенные правила не проверены: " + strings.Join(rules, "; ") + ". Укажите недостающие данные для окончательного результата."
}
