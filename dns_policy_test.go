package main

import "testing"

func TestDNSPolicyOrderedFallback(t *testing.T) {
	configDatabase(t)
	saveSetting("dns_mode", "rules")
	saveSetting("dns_vpn_servers", "https://1.1.1.1/dns-query\nhttps://8.8.8.8/dns-query")
	servers, routes, e := dnsPolicy("1.1.1.1", "direct", []Rule{{ID: 1, Kind: "domain", Value: "domain:example.com", Target: "proxy"}, {ID: 2, Kind: "domain", Value: "domain:example.com", Target: "direct"}, {ID: 3, Kind: "domain", Value: "domain:private.test", Target: "proxy", Source: "192.168.1.56"}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(routes) != 2 {
		t.Fatal("device rule must not affect global DNS")
	}
	if servers[0].(map[string]any)["finalQuery"] != false || servers[1].(map[string]any)["finalQuery"] != true {
		t.Fatal("reserve group must end before later overlapping rule")
	}
	if routes[0].(map[string]any)["outboundTag"] != "block" {
		t.Fatal("VPN DNS escaped to direct")
	}
}
func TestDNSRejectsBypassAndIPv6(t *testing.T) {
	for _, s := range []string{"https+local://dns.google/dns-query", "tls://1.1.1.1", "https://[::1]/dns-query", "https://user:password@example.com/dns-query"} {
		if _, e := normalizeDNSServers(s, ""); e == nil {
			t.Fatal(s)
		}
	}
}
