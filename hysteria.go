package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func nodeOutbound(raw string) (map[string]any, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return nil, fmt.Errorf("Некорректная ссылка подключения")
	}
	if u.Scheme == "hy2" || u.Scheme == "hysteria2" {
		return hysteriaOutbound(u)
	}
	return vlessOutbound(raw)
}
func hysteriaOutbound(u *url.URL) (map[string]any, error) {
	if u.User == nil || u.User.String() == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("Hysteria 2: отсутствует пароль или сервер")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && ip.To4() == nil {
		return nil, fmt.Errorf("IPv6-серверы исключены из этой версии")
	}
	port := 443
	var e error
	if u.Port() != "" {
		port, e = strconv.Atoi(u.Port())
	}
	if e != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("Некорректный порт Hysteria 2")
	}
	auth := u.User.Username()
	if password, ok := u.User.Password(); ok {
		auth += ":" + password
	}
	q := u.Query()
	for _, key := range []string{"mport", "ports", "upmbps", "downmbps"} {
		if q.Get(key) != "" {
			return nil, fmt.Errorf("Hysteria 2: параметры смены портов и скорости в URL пока не поддерживаются")
		}
	}
	tls := map[string]any{"serverName": q.Get("sni"), "allowInsecure": q.Get("insecure") == "1" || q.Get("insecure") == "true"}
	if tls["serverName"] == "" {
		tls["serverName"] = u.Hostname()
	}
	if q.Get("alpn") != "" {
		tls["alpn"] = strings.Split(q.Get("alpn"), ",")
	}
	if q.Get("pinSHA256") != "" {
		tls["pinnedPeerCertSha256"] = q.Get("pinSHA256")
	}
	fm := map[string]any{}
	if raw := q.Get("fm"); raw != "" {
		if len(raw) > 65536 || json.Unmarshal([]byte(raw), &fm) != nil || fm == nil {
			return nil, fmt.Errorf("Hysteria 2: FinalMask должен быть JSON-объектом")
		}
		for key := range fm {
			if key != "udp" && key != "quicParams" {
				return nil, fmt.Errorf("Hysteria 2: неизвестные параметры FinalMask")
			}
		}
		if udp, ok := fm["udp"]; ok {
			masks, ok := udp.([]any)
			if !ok {
				return nil, fmt.Errorf("Некорректный UDP FinalMask")
			}
			for _, v := range masks {
				m, ok := v.(map[string]any)
				if !ok || m["type"] != "salamander" {
					return nil, fmt.Errorf("Поддерживается только маскировка Salamander")
				}
			}
		}
	}
	if obfs := q.Get("obfs"); obfs != "" && obfs != "none" {
		if obfs != "salamander" || q.Get("obfs-password") == "" {
			return nil, fmt.Errorf("Hysteria 2: требуется пароль Salamander")
		}
		if _, exists := fm["udp"]; !exists {
			fm["udp"] = []any{map[string]any{"type": "salamander", "settings": map[string]any{"password": q.Get("obfs-password")}}}
		}
	}
	stream := map[string]any{"network": "hysteria", "security": "tls", "tlsSettings": tls, "hysteriaSettings": map[string]any{"version": 2, "auth": auth}, "sockopt": map[string]any{"mark": 666, "domainStrategy": "UseIPv4"}}
	if len(fm) > 0 {
		stream["finalmask"] = fm
	}
	return map[string]any{"tag": "proxy", "protocol": "hysteria", "settings": map[string]any{"version": 2, "address": u.Hostname(), "port": port}, "streamSettings": stream}, nil
}
