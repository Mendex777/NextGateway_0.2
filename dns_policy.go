package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func normalizeDNSServers(raw, fallback string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = fallback
	}
	values := strings.Fields(raw)
	if len(values) > 8 {
		return "", fmt.Errorf("Не более 8 DNS-серверов в списке")
	}
	for _, v := range values {
		if validIPv4(v) {
			continue
		}
		u, e := url.Parse(v)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return "", fmt.Errorf("DNS: укажите IPv4 или HTTPS URL DoH; DoT пока не поддерживается")
		}
		if ip := net.ParseIP(u.Hostname()); ip != nil && ip.To4() == nil {
			return "", fmt.Errorf("IPv6 DNS не поддерживается")
		}
	}
	return strings.Join(values, "\n"), nil
}
func dnsDefaultTarget(mode string) string {
	switch setting("dns_mode") {
	case "proxy":
		return "proxy"
	case "rules":
		return mode
	}
	return "direct"
}
func dnsPolicy(bootstrap, mode string, rules []Rule, hosts []string) ([]any, []any, error) {
	direct, e := normalizeDNSServers(setting("dns_direct_servers"), bootstrap)
	if e != nil {
		return nil, nil, e
	}
	vpn, e := normalizeDNSServers(setting("dns_vpn_servers"), "https://1.1.1.1/dns-query")
	if e != nil {
		return nil, nil, e
	}
	// Resolver hostnames must be resolved without depending on their own DoH connection.
	for _, raw := range strings.Fields(direct + "\n" + vpn) {
		u, _ := url.Parse(raw)
		if u != nil && u.Hostname() != "" && net.ParseIP(u.Hostname()) == nil {
			hosts = append(hosts, "full:"+u.Hostname())
		}
	}
	servers := []any{}
	routes := []any{}
	if len(hosts) > 0 {
		servers = append(servers, map[string]any{"address": bootstrap, "domains": hosts, "skipFallback": true, "finalQuery": true, "tag": "dns-bootstrap"})
	}
	add := func(raw, tag string, domains []string) {
		list := strings.Fields(raw)
		for i, address := range list {
			entry := map[string]any{"address": address, "tag": tag, "timeoutMs": 3000}
			if len(domains) > 0 {
				entry["domains"] = domains
				entry["skipFallback"] = true
				entry["finalQuery"] = i == len(list)-1
			}
			servers = append(servers, entry)
		}
	}
	if setting("dns_mode") == "rules" {
		for _, r := range rules {
			if r.Disabled || r.Kind != "domain" || r.Source != "" {
				continue
			}
			domains := strings.FieldsFunc(r.Value, func(c rune) bool { return c == '\n' || c == ',' })
			if len(domains) == 0 {
				continue
			}
			tag := fmt.Sprintf("dns-rule-%d", r.ID)
			raw := vpn
			if r.Target == "direct" {
				raw = direct
			}
			add(raw, tag, domains)
			routes = append(routes, map[string]any{"type": "field", "inboundTag": []string{tag}, "outboundTag": targetTag(r.Target)})
		}
	}
	raw := direct
	if target := dnsDefaultTarget(mode); target == "proxy" || strings.HasPrefix(target, "group:") || strings.HasPrefix(target, "node:") {
		raw = vpn
	}
	add(raw, "dns-upstream", nil)
	return servers, routes, nil
}

// An empty override follows the VM resolver configuration at apply time.
func bootstrapDNS() string {
	if override := strings.TrimSpace(setting("dns_direct")); override != "" {
		return override
	}
	network, err := detectNetwork()
	if err == nil {
		for _, server := range strings.Fields(network.SystemDNS) {
			if validIPv4(server) {
				return server
			}
		}
	}
	return ""
}
