package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
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
	if dnsDefaultTarget(mode) == "proxy" {
		raw = vpn
	}
	add(raw, "dns-upstream", nil)
	return servers, routes, nil
}

func diagnoseDNS() (string, error) {
	start := time.Now()
	c, e := net.DialTimeout("udp4", "127.0.0.1:1053", time.Second)
	if e != nil {
		return "", e
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(8 * time.Second))
	id := uint16(time.Now().UnixNano())
	q := make([]byte, 12)
	binary.BigEndian.PutUint16(q, id)
	q[2] = 1
	q[5] = 1
	for _, part := range strings.Split("example.com", ".") {
		q = append(q, byte(len(part)))
		q = append(q, part...)
	}
	q = append(q, 0, 0, 1, 0, 1)
	if _, e = c.Write(q); e != nil {
		return "", e
	}
	b := make([]byte, 4096)
	n, e := c.Read(b)
	if e != nil {
		return "", fmt.Errorf("DNS не ответил: %v", e)
	}
	if n < 12 || binary.BigEndian.Uint16(b) != id || b[2]&128 == 0 {
		return "", fmt.Errorf("Некорректный ответ DNS")
	}
	if b[3]&15 != 0 {
		return "", fmt.Errorf("DNS вернул код ошибки %d", b[3]&15)
	}
	count := binary.BigEndian.Uint16(b[6:8])
	if count == 0 {
		return "", fmt.Errorf("DNS ответил без адресов")
	}
	return fmt.Sprintf("Работающий DNS Xray: example.com, ответов %d, %d мс (возможен ответ из кеша)", count, time.Since(start).Milliseconds()), nil
}
