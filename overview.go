package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type ComponentStatus struct {
	Name, State, Class, Link, Action, Button, Hint string
	Update                                         string
}

func componentOverview(p Page) []ComponentStatus {
	rows := []ComponentStatus{}
	add := func(name, state, class, link, action, button, hint string) {
		rows = append(rows, ComponentStatus{Name: name, State: state, Class: class, Link: link, Action: action, Button: button, Hint: hint})
	}
	add("Панель", p.PanelVersion+" · работает", "good", "", "panel-update-check", "Проверить обновления", "Обновление панели не останавливает Xray")
	network := "Определена · " + p.DetectedNetwork.Address + " · роутер " + p.DetectedNetwork.Router
	class := "neutral"
	if p.Runtime.Network == "pending" {
		network = "Ожидает подтверждения"
		class = "warn"
	} else if p.Runtime.Network == "direct" {
		network = "Выход через роутер подтверждён · " + p.DetectedNetwork.Address
		class = "good"
	}
	if p.DetectedNetwork.Address == "" {
		network = "Не удалось определить сеть"
		class = "warn"
	}
	add("Сеть ВМ", network, class, "/?tab=gateway", "", "Настроить", "DHCP допустим; закрепите адрес ВМ на роутере")
	version := strings.Fields(p.Version)
	xray := "Не установлен"
	class = "warn"
	if len(version) > 1 && version[0] == "Xray" {
		xray = "Установлен · " + version[1]
		class = "good"
	}
	add("Xray", xray, class, "", "install", "Установить / обновить", "Обновление работающего Xray требует предварительной остановки службы")
	if len(version) > 1 && newerCore(setting("xray_latest"), version[1]) {
		rows[len(rows)-1].Update = "Доступна " + setting("xray_latest")
	}
	service := strings.TrimSpace(p.Service)
	class = "warn"
	if service == "active" {
		service = "Работает"
		class = "good"
	} else {
		service = "Остановлена"
	}
	add("Служба Xray", service, class, "", "start", "Запустить", "")
	missing := []string{}
	for _, binary := range []string{"nft", "ip", "curl", "avahi-resolve-address"} {
		if _, e := exec.LookPath(binary); e != nil {
			missing = append(missing, binary)
		}
	}
	deps := "Установлены"
	class = "good"
	if len(missing) > 0 {
		deps = "Не установлены: " + strings.Join(missing, ", ")
		class = "warn"
	}
	add("Компоненты шлюза", deps, class, "", "dependencies", "Установить", "")
	gateway := "Выключен"
	class = "neutral"
	if p.Runtime.Gateway {
		gateway = "Применён · TPROXY / nftables"
		class = "good"
		if p.GatewayHealth != "" || strings.TrimSpace(p.Service) != "active" {
			gateway = "Требует проверки"
			class = "warn"
		}
	}
	add("Перехват LAN", gateway, class, "/?tab=gateway", "", "Настроить", "Состояние применённой конфигурации; подробная проверка — в диагностике")
	dns := "Не проверен"
	class = "neutral"
	if stamp, e := time.Parse(time.RFC3339, setting("dns_last_check_time")); e == nil {
		dns = "Ошибка проверки · " + stamp.Local().Format("02.01 15:04")
		if setting("dns_last_check_ok") == "1" {
			dns = "Отвечает · " + stamp.Local().Format("02.01 15:04")
		}
		class = "warn"
		if setting("dns_last_check_ok") == "1" && time.Since(stamp) < 10*time.Minute && strings.TrimSpace(p.Service) == "active" {
			class = "good"
		} else {
			dns += " · нужна новая проверка"
		}
	}
	add("DNS для устройств", dns, class, "", "dns-diagnose", "Проверить", "Проверяется локальный DNS Xray; это не проверка настроек клиентского устройства")
	geo := "Не установлены"
	class = "warn"
	if a, e := os.Stat("/usr/local/share/ngpanel-geodata/geosite.dat"); e == nil {
		if _, e = os.Stat("/usr/local/share/ngpanel-geodata/geoip.dat"); e == nil {
			geo = "Установлены · " + a.ModTime().Local().Format("02.01.2006")
			class = "good"
		}
	}
	add("Базы geosite / geoip", geo, class, "", "geodata", "Обновить", "")
	config := "Применена"
	class = "good"
	if p.Runtime.ConfigHash == "" {
		config = "Не применена"
		class = "warn"
	} else if p.Pending {
		config = "Есть неприменённые изменения"
		class = "warn"
	}
	if p.ConfigError != "" {
		config = p.ConfigError
		class = "bad"
	}
	add("Конфигурация", config, class, "", "apply", "Применить", "Применение перезапускает Xray и может прервать соединения")
	return rows
}
func overallReadiness(p Page) string {
	if p.Runtime.Gateway && strings.TrimSpace(p.Service) == "active" && p.GatewayHealth == "" {
		if p.Pending {
			return "Шлюз работает · есть неприменённые изменения"
		}
		return "Шлюз работает"
	}
	return fmt.Sprint("Шлюз ещё не готов к работе")
}

func newerCore(latest, current string) bool {
	parse := func(raw string) ([3]int, bool) {
		var out [3]int
		parts := strings.Split(strings.TrimPrefix(raw, "v"), ".")
		if len(parts) != 3 {
			return out, false
		}
		for i, part := range parts {
			n, e := strconv.Atoi(part)
			if e != nil {
				return out, false
			}
			out[i] = n
		}
		return out, true
	}
	a, ok := parse(latest)
	b, valid := parse(current)
	if !ok || !valid {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
func checkCoreUpdate() (string, error) {
	client := http.Client{Timeout: 15 * time.Second}
	request, _ := http.NewRequest("GET", "https://api.github.com/repos/XTLS/Xray-core/releases/latest", nil)
	request.Header.Set("User-Agent", "NGPanel")
	response, e := client.Do(request)
	if e != nil {
		return "", fmt.Errorf("Не удалось проверить обновления Xray")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("GitHub не ответил на проверку Xray: %d", response.StatusCode)
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool
		Prerelease bool
	}
	if json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(&release) != nil || release.Draft || release.Prerelease || !newerCore(release.Tag, "0.0.0") {
		return "", fmt.Errorf("Некорректные сведения о релизе Xray")
	}
	saveSetting("xray_latest", release.Tag)
	return "Последний стабильный Xray: " + release.Tag, nil
}
