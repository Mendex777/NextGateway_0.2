package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type OperationStatus struct {
	State, Message string
	Done           bool
}

func operationKind(action string) string {
	switch action {
	case "panel-update-check", "panel-update-install", "panel-update-rollback":
		return "panel"
	case "install":
		return "install"
	case "check", "apply", "start", "restart", "stop", "rollback", "network", "network-confirm", "logs", "geodata", "dependencies":
		return "control"
	}
	return ""
}
func operationStatus(kind, action string, since time.Time) OperationStatus {
	pending := OperationStatus{State: "running", Message: "Выполняется операция…"}
	switch kind {
	case "devices":
		message := setting("device_discovery")
		if message == "" || strings.Contains(message, "выполняется") {
			return pending
		}
		state := "ok"
		if strings.HasPrefix(message, "Не удалось") || strings.HasPrefix(message, "Некорректный") {
			state = "error"
		}
		return OperationStatus{State: state, Message: message, Done: true}
	case "probe":
		batchMu.Lock()
		s := batchStatus
		batchMu.Unlock()
		if s.State == "running" {
			pending.Message = fmt.Sprintf("Проверяются подключения: %d из %d", s.Done, s.Total)
			return pending
		}
		if s.State == "" {
			return OperationStatus{State: "error", Message: "Проверка прервана перезапуском панели", Done: true}
		}
		message := fmt.Sprintf("Проверка завершена: доступны %d из %d подключений", s.OK, s.Total)
		if s.State == "cancelled" {
			message = fmt.Sprintf("Проверка отменена: проверено %d из %d", s.Done, s.Total)
		}
		return OperationStatus{State: "ok", Message: message, Done: true}

	case "panel":
		if _, e := os.Stat(filepath.Join(stateDir(), "jobs/panel-update.request")); e == nil {
			pending.Message = "Обработка обновления панели…"
			return pending
		}
		raw, e := os.ReadFile(filepath.Join(stateDir(), "panel-update.json"))
		if e != nil {
			return pending
		}
		var s struct{ State, Message, Updated string }
		if json.Unmarshal(raw, &s) != nil {
			return pending
		}
		stamp, _ := time.Parse(time.RFC3339Nano, s.Updated)
		if stamp.Before(since) || s.State == "running" {
			return pending
		}
		return OperationStatus{State: s.State, Message: s.Message, Done: true}
	case "install":
		if _, e := os.Stat(filepath.Join(stateDir(), "jobs/install.request")); e == nil {
			pending.Message = "Устанавливается Xray…"
			return pending
		}
		path := filepath.Join(stateDir(), "install-status")
		info, e := os.Stat(path)
		if e != nil || info.ModTime().Before(since) {
			return pending
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return pending
		}
		s := string(raw)
		state := "ok"
		if strings.HasPrefix(s, "Ошибка") {
			state = "error"
		}
		return OperationStatus{State: state, Message: s, Done: true}
	case "control":
		if _, e := os.Stat(filepath.Join(stateDir(), "jobs/control.request")); e == nil {
			return pending
		}
		s := readRuntime()
		stamp, _ := time.Parse(time.RFC3339Nano, s.Updated)
		if s.Action != action || stamp.Before(since) || s.State == "running" {
			return pending
		}
		return OperationStatus{State: s.State, Message: s.Message, Done: true}
	}
	return OperationStatus{State: "error", Message: "Неизвестная операция", Done: true}
}
func operationHandler(w http.ResponseWriter, r *http.Request) {
	since, e := time.Parse(time.RFC3339Nano, r.URL.Query().Get("since"))
	action := r.URL.Query().Get("action")
	kind := operationKind(action)
	if e != nil || kind == "" {
		http.Error(w, "Invalid operation", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(operationStatus(kind, action, since))
}
