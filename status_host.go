package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The health collector owns system commands; HTTP handlers only read its snapshot.
type statusHostSnapshot struct {
	Network                                                               GatewayNetwork
	NetworkError, Version, Service, Routes, Memory, Uptime, GatewayHealth string
	XrayPID                                                               int
	ActiveSince                                                           uint64
}

func detectedHealthNetwork(addresses, routes []byte) (GatewayNetwork, error) {
	var rs []struct {
		Dst, Dev, Gateway string
		Metric            int
	}
	var links []struct {
		Ifname    string
		Addresses []struct {
			Local  string
			Prefix int `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(routes, &rs); err != nil {
		return GatewayNetwork{}, err
	}
	if err := json.Unmarshal(addresses, &links); err != nil {
		return GatewayNetwork{}, err
	}
	n := GatewayNetwork{}
	metric := int(^uint(0) >> 1)
	for _, route := range rs {
		if route.Dst == "default" && route.Dev != "lo" && route.Gateway != "" && route.Metric < metric {
			n.Interface, n.Router, metric = route.Dev, route.Gateway, route.Metric
		}
	}
	for _, link := range links {
		if link.Ifname != n.Interface {
			continue
		}
		for _, a := range link.Addresses {
			_, subnet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", a.Local, a.Prefix))
			if err == nil && net.ParseIP(a.Local).To4() != nil && subnet.Contains(net.ParseIP(n.Router)) {
				n.Address, n.CIDR = a.Local, subnet.String()
				n.SystemDNS = systemDNSServers()
				return n, n.validate()
			}
		}
	}
	return n, fmt.Errorf("Не удалось определить адрес и маршрут LAN")
}

func healthHostSnapshot(e healthEvidence, h ServiceHealth) *statusHostSnapshot {
	s := &statusHostSnapshot{Version: e.Version, Service: strings.TrimSpace(e.Active), Routes: string(e.Routes), XrayPID: e.XrayPID, ActiveSince: e.ActiveSince}
	var err error
	s.Network, err = detectedHealthNetwork(e.Addresses, e.Routes)
	if err != nil {
		s.NetworkError = err.Error()
	}
	mem := procNumbers("/proc/meminfo")
	s.Memory = fmt.Sprintf("Mem: %d MiB total, %d MiB available", mem["MemTotal"]/1048576, mem["MemAvailable"]/1048576)
	if raw, err := os.ReadFile("/proc/uptime"); err == nil {
		if fields := strings.Fields(string(raw)); len(fields) > 0 {
			seconds, _ := strconv.ParseFloat(fields[0], 64)
			s.Uptime = (time.Duration(seconds) * time.Second).String()
		}
	}
	if e.Runtime.Gateway {
		for _, check := range h.Checks {
			if check.ID == "routing" && check.State != "good" {
				s.GatewayHealth = check.Summary
			}
		}
	}
	return s
}

func currentStatusHost(h ServiceHealth, now time.Time) *statusHostSnapshot {
	if h.Host == nil || h.Updated.IsZero() || now.Sub(h.Updated) > 45*time.Second || h.Updated.Sub(now) > 5*time.Second {
		return nil
	}
	return h.Host
}
func populateStatusHost(p *Page) {
	h := readServiceHealth()
	s := currentStatusHost(h, time.Now())
	if s == nil {
		p.DetectedNetwork = p.Runtime.AppliedNetwork
		if p.DetectedNetwork.Address == "" {
			p.DetectedNetwork = p.Network
		}
		p.Service = "unknown"
		p.NetworkError = "Нет актуального снимка состояния сети"
		if p.Runtime.Gateway {
			p.GatewayHealth = "Нет актуального снимка проверки шлюза"
		}
		return
	}
	p.DetectedNetwork, p.NetworkError = s.Network, s.NetworkError
	p.Version, p.Service, p.Routes, p.Memory, p.Uptime, p.GatewayHealth = s.Version, s.Service, s.Routes, s.Memory, s.Uptime, s.GatewayHealth
}

func healthFileIdentity(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return path + ":missing"
	}
	identity := fmt.Sprint(path, info.Size(), info.Mode(), info.ModTime().UnixNano())
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		identity += fmt.Sprint(stat.Uid, stat.Gid, stat.Ino, stat.Dev)
	}
	return identity
}
func healthValidationSignature(config []byte, version, account string, paths []string) string {
	signature := fmt.Sprintf("%x:%s:%s", sha256.Sum256(config), version, account)
	for _, path := range paths {
		signature += "|" + healthFileIdentity(filepath.Clean(path))
	}
	return signature
}
func (c *healthCollector) validationDue(signature string, now time.Time) bool {
	interval := 5 * time.Minute
	if !c.valid {
		interval = time.Minute
	}
	return c.tested.IsZero() || signature != c.signature || now.Sub(c.tested) >= interval
}
