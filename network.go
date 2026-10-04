package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"regexp"
)

type GatewayNetwork struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	CIDR      string `json:"cidr"`
	Router    string `json:"router"`
}

func (n GatewayNetwork) validate() error {
	if !regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`).MatchString(n.Interface) || n.Interface == "lo" {
		return fmt.Errorf("Укажите сетевой интерфейс LAN")
	}
	ip, subnet, e := net.ParseCIDR(n.CIDR)
	if e != nil || ip.To4() == nil || subnet.String() != n.CIDR {
		return fmt.Errorf("Укажите подсеть IPv4 в формате CIDR")
	}
	ones, _ := subnet.Mask.Size()
	address, router := net.ParseIP(n.Address), net.ParseIP(n.Router)
	if ones < 1 || ones > 30 || address == nil || router == nil || address.To4() == nil || router.To4() == nil || !subnet.Contains(address) || !subnet.Contains(router) || address.Equal(router) {
		return fmt.Errorf("Адрес ВМ и роутер должны быть разными IPv4 в подсети LAN")
	}
	broadcast := append(net.IP(nil), subnet.IP.To4()...)
	for i := range broadcast {
		broadcast[i] |= ^subnet.Mask[i]
	}
	if address.Equal(subnet.IP) || router.Equal(subnet.IP) || address.Equal(broadcast) || router.Equal(broadcast) {
		return fmt.Errorf("Адрес сети и broadcast нельзя использовать для ВМ или роутера")
	}
	return nil
}

func detectNetwork() (GatewayNetwork, error) {
	var routes []struct {
		Dev     string `json:"dev"`
		Gateway string `json:"gateway"`
		Metric  int    `json:"metric"`
	}
	raw, e := exec.Command("ip", "-j", "-4", "route", "show", "default").Output()
	if e != nil {
		return GatewayNetwork{}, fmt.Errorf("Не удалось определить сеть: %w", e)
	}
	if e = json.Unmarshal(raw, &routes); e != nil {
		return GatewayNetwork{}, e
	}
	var n GatewayNetwork
	metric := int(^uint(0) >> 1)
	for _, r := range routes {
		if r.Gateway != "" && r.Dev != "lo" && r.Metric < metric {
			n.Interface = r.Dev
			n.Router = r.Gateway
			metric = r.Metric
		}
	}
	if n.Interface == "" {
		return n, fmt.Errorf("Маршрут по умолчанию не найден; заполните параметры вручную")
	}
	raw, e = exec.Command("ip", "-j", "-4", "addr", "show", "dev", n.Interface).Output()
	if e != nil {
		return n, e
	}
	var links []struct {
		Addresses []struct {
			Local  string `json:"local"`
			Prefix int    `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if e = json.Unmarshal(raw, &links); e != nil {
		return n, e
	}
	for _, l := range links {
		for _, a := range l.Addresses {
			_, subnet, e := net.ParseCIDR(fmt.Sprintf("%s/%d", a.Local, a.Prefix))
			if e == nil && subnet.Contains(net.ParseIP(n.Router)) {
				n.Address = a.Local
				n.CIDR = subnet.String()
				return n, n.validate()
			}
		}
	}
	return n, fmt.Errorf("Не найден адрес LAN для текущего роутера")
}

func gatewayNetwork() GatewayNetwork {
	var n GatewayNetwork
	if db != nil {
		if json.Unmarshal([]byte(setting("gateway_network")), &n) == nil {
			return n
		}
	}
	n, _ = detectNetwork()
	return n
}

func saveNetwork(n GatewayNetwork) error {
	if e := n.validate(); e != nil {
		return e
	}
	raw, _ := json.Marshal(n)
	return saveSetting("gateway_network", string(raw))
}
