package main

import _ "embed"

// Keep the stylesheet in the binary so existing release installers need no new assets.
//
//go:embed ui.css
var panelCSS string

func (p Page) Title() string {
	titles := map[string]string{
		"status": "Главная", "subscriptions": "Подписки и подключения",
		"devices": "Устройства", "routing": "Маршрутизация", "gateway": "DNS и шлюз",
		"diagnostics": "Диагностика", "backup": "Бекап",
	}
	if title := titles[p.Tab]; title != "" {
		return title
	}
	return "NGPanel"
}
