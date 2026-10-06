package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReactBackupRestoreResponse(t *testing.T) {
	backupDB(t)
	raw, err := exportBackup()
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	f, err := form.CreateFormFile("backup", "settings.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Write(raw)
	form.Close()
	req := httptest.NewRequest("POST", "http://panel.test/backup", &body)
	req.Header.Set("Origin", "http://panel.test")
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	var response map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response["ok"] != true {
		t.Fatalf("JSON restore failed: %d %s", w.Code, w.Body.String())
	}
}

func TestReactRouterAndLegacyStatus(t *testing.T) {
	backupDB(t)
	router := newRouter()
	for _, path := range []string{"/", "/probe-status", "/group-check-status?id=missing", "/LICENSE"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatal("missing CSP")
		}
		if path == "/" && !strings.Contains(w.Body.String(), "/assets/") {
			t.Fatal("React entry missing")
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/page?tab=unexpected", nil))
	if w.Code != 400 {
		t.Fatal("invalid tab accepted")
	}
}

func TestJSONActionAndOrigin(t *testing.T) {
	backupDB(t)
	router := newRouter()
	body := url.Values{"action": {"setup-skip"}, "tab": {"status"}}.Encode()
	req := httptest.NewRequest("POST", "http://panel.test/api/action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://panel.test")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var response map[string]any
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || response["ok"] != true || setting("setup_skipped") != "1" {
		t.Fatalf("action failed: %d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest("POST", "http://panel.test/api/action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://other.test")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin action allowed: %d", w.Code)
	}
}

func TestReviewDisablesSystemOperations(t *testing.T) {
	t.Setenv("NG_REVIEW", "1")
	router := newRouter()
	req := httptest.NewRequest("POST", "/api/action", strings.NewReader("action=apply&tab=routing"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("review allows apply: %d", w.Code)
	}
}
