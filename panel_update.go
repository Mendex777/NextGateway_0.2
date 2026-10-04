package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type PanelUpdateStatus struct {
	State, Message, Latest string
	Available, CanRollback bool
}

func readPanelUpdate() PanelUpdateStatus {
	s := PanelUpdateStatus{Message: "Обновления ещё не проверены"}
	b, e := os.ReadFile(filepath.Join(stateDir(), "panel-update.json"))
	if e == nil {
		json.Unmarshal(b, &s)
	}
	return s
}
func requestPanelUpdate(action string) error {
	if action != "check" && action != "install" && action != "rollback" {
		return fmt.Errorf("Неизвестное действие")
	}

	dir := filepath.Join(stateDir(), "jobs")
	name := filepath.Join(dir, "panel-update.request")
	f, e := os.CreateTemp(dir, ".panel-update-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = json.NewEncoder(f).Encode(map[string]string{"action": action}); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Link(f.Name(), name); e != nil {
		return fmt.Errorf("Обновление уже выполняется или очередь недоступна")
	}
	return nil
}
