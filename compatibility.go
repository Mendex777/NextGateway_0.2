package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func validateEncryption(value string) error {
	if value == "none" {
		return nil
	}
	parts := strings.Split(value, ".")
	invalid := fmt.Errorf("Некорректный формат VLESS Encryption (ML-KEM)")
	if len(parts) < 4 || parts[0] != "mlkem768x25519plus" {
		return invalid
	}
	if parts[1] != "native" && parts[1] != "xorpub" && parts[1] != "random" {
		return invalid
	}
	if parts[2] != "0rtt" && parts[2] != "1rtt" {
		return invalid
	}
	keys := 0
	for _, part := range parts[3:] {
		if len(part) < 20 {
			continue
		}
		b, e := base64.RawURLEncoding.DecodeString(part)
		if e != nil || (len(b) != 32 && len(b) != 1184) {
			return invalid
		}
		keys++
	}
	if keys == 0 {
		return invalid
	}
	return nil
}

func xhttpSettings(q url.Values) (map[string]any, error) {
	mode := q.Get("mode")
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "auto", "packet-up", "stream-up", "stream-one":
	default:
		return nil, fmt.Errorf("Неизвестный режим XHTTP")
	}
	extra := map[string]any{}
	if raw := q.Get("extra"); raw != "" {
		if len(raw) > 65536 {
			return nil, fmt.Errorf("XHTTP extra превышает 64 КБ")
		}
		if json.Unmarshal([]byte(raw), &extra) != nil {
			return nil, fmt.Errorf("XHTTP extra должен быть JSON-объектом")
		}
		if extra == nil {
			extra = map[string]any{}
		}
	}
	// Some clients export empty fields from a newer core. Non-default values
	// must be rejected, rather than silently losing settings on this core.
	for _, key := range []string{"sessionIDLength", "sessionIDTable", "sessionIDKey", "sessionIDPlacement"} {
		if v, ok := extra[key]; ok {
			if v != nil && v != "" && v != "0" && v != float64(0) {
				return nil, fmt.Errorf("Параметры sessionID XHTTP требуют более новой версии Xray; эта версия панели их не поддерживает")
			}
			delete(extra, key)
		}
	}
	allowed := strings.Fields("host path mode headers xPaddingBytes xPaddingObfsMode xPaddingKey xPaddingHeader xPaddingPlacement xPaddingMethod uplinkHTTPMethod sessionPlacement sessionKey seqPlacement seqKey uplinkDataPlacement uplinkDataKey uplinkChunkSize noGRPCHeader noSSEHeader scMaxEachPostBytes scMinPostsIntervalMs scMaxBufferedPosts scStreamUpServerSecs serverMaxHeaderBytes xmux")
	for key, v := range extra {
		if (key == "downloadSettings" || key == "extra") && v == nil {
			delete(extra, key)
			continue
		}
		found := false
		for _, k := range allowed {
			if key == k {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("XHTTP extra содержит неподдерживаемые параметры")
		}
	}
	if padding := q.Get("x_padding_bytes"); padding != "" {
		if _, exists := extra["xPaddingBytes"]; !exists {
			extra["xPaddingBytes"] = padding
		}
	}
	return map[string]any{"host": q.Get("host"), "path": q.Get("path"), "mode": mode, "extra": extra}, nil
}

func fillNodeDetails(n *Node, raw string) {
	u, e := url.Parse(raw)
	if e != nil {
		n.Compatibility = "Некорректная VLESS-ссылка"
		return
	}
	q := u.Query()
	n.SNI = q.Get("sni")
	n.Flow = q.Get("flow")
	n.Path = q.Get("path")
	if n.Transport == "raw" {
		n.Transport = "tcp"
	}
	if n.Security == "" {
		n.Security = "none"
	}
	n.Protocol = "VLESS"
	if u.Scheme == "hysteria2" || u.Scheme == "hy2" {
		n.Protocol = "Hysteria 2"
		n.Transport = "QUIC / UDP"
		n.Security = "tls"
		if q.Get("insecure") == "1" || q.Get("insecure") == "true" {
			n.Security = "tls (без проверки сертификата)"
		}
	}
	n.Encryption = "none"
	if q.Get("encryption") != "" && q.Get("encryption") != "none" {
		n.Encryption = "ML-KEM / X25519"
	}
	if _, e := nodeOutbound(raw); e != nil {
		n.Compatibility = e.Error()
	}
}

func validateXrayOutbound(out map[string]any) error {
	if _, e := os.Stat("/usr/local/bin/xray"); e != nil {
		return fmt.Errorf("Установите Xray перед выбором подключения")
	}
	dir, e := os.MkdirTemp(filepath.Join(stateDir(), "db"), ".validate-")
	if e != nil {
		return fmt.Errorf("Не удалось подготовить проверку совместимости")
	}
	defer os.RemoveAll(dir)
	b, _ := json.Marshal(map[string]any{"log": map[string]any{"loglevel": "none"}, "outbounds": []any{out}})
	path := filepath.Join(dir, "config.json")
	if os.WriteFile(path, b, 0600) != nil {
		return fmt.Errorf("Не удалось записать проверочную конфигурацию")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if exec.CommandContext(ctx, "/usr/local/bin/xray", "run", "-test", "-c", path).Run() != nil {
		return fmt.Errorf("Установленный Xray отклонил параметры подключения. Выбранный выход не изменён")
	}
	return nil
}
