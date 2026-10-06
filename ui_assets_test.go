package main

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStylesheetIsServedAndAllowed(t *testing.T) {
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest("GET", "/ui.css", nil))
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") || !strings.Contains(w.Body.String(), ".sidebar") {
		t.Fatal("stylesheet is unavailable")
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "style-src 'self'") {
		t.Fatal("same-origin stylesheet blocked by policy")
	}
}

func TestOverviewActionsFollowReadiness(t *testing.T) {
	var out bytes.Buffer
	components := []ComponentStatus{
		{Name: "Служба Xray", Class: "good", Action: "start", Button: "Запустить"},
		{Name: "Компоненты шлюза", Class: "good", Action: "dependencies", Button: "Установить"},
		{Name: "Конфигурация", Class: "good", Action: "apply", Button: "Применить"},
	}
	if err := pageTemplate().Execute(&out, Page{Tab: "status", Components: components, GatewayReady: true}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"start", "dependencies", "apply"} {
		componentsHTML := strings.Split(strings.Split(out.String(), `<table id="component-overview">`)[1], `</table>`)[0]
		if strings.Contains(componentsHTML, `name="action" value="`+action+`"`) {
			t.Fatalf("redundant ready action: %s", action)
		}
	}
	components[0].Class = "warn"
	out.Reset()
	if err := pageTemplate().Execute(&out, Page{Tab: "status", Components: components}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `name="action" value="start"`) {
		t.Fatal("stopped service cannot be started")
	}
}

func TestPendingChangesVisibleAcrossPages(t *testing.T) {
	for _, tab := range []string{"status", "subscriptions", "devices", "routing", "gateway", "diagnostics", "backup"} {
		var out bytes.Buffer
		if err := pageTemplate().Execute(&out, Page{Tab: tab, Pending: true, Runtime: Runtime{ConfigHash: "applied"}}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Есть неприменённые изменения") {
			t.Fatalf("pending state hidden on %s", tab)
		}
	}
}
