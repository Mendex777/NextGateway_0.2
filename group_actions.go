package main

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

func selectGroupNode(id, node string) error {
	if nodeDisabled(node) {
		return fmt.Errorf("Подписка отключена")
	}
	groupActionLock.Lock()
	defer groupActionLock.Unlock()
	runtime := readRuntime()
	if runtime.State == "running" || !runtime.Balance {
		return fmt.Errorf("Сначала примените конфигурацию с группой")
	}
	var applied *BalanceGroup
	for _, g := range runtime.Groups {
		if g.ID == id {
			copy := g
			applied = &copy
		}
	}
	if applied == nil || !slices.Contains(applied.Nodes, node) {
		return fmt.Errorf("Узел отсутствует в применённой группе; примените сохранённые настройки")
	}
	all, e := observations()
	if e != nil {
		return fmt.Errorf("API Xray недоступен")
	}
	tag := "auto-vpn-" + id + "-" + node + "-"
	if _, ok := all[tag]; !ok {
		return fmt.Errorf("Узел ещё не проверен Xray; дождитесь первой проверки")
	}
	if e = overrideGroup(id, tag); e != nil {
		return fmt.Errorf("Не удалось переключить группу через API Xray")
	}
	groupControl.Lock()
	// A manual switch must not introduce an extra cooldown. The next new
	// observation resumes the existing policy, including its failure count.
	groupControl.states[id] = groupDecision{Current: tag, Manual: true, ManualChecked: all[tag].Checked, Reason: "Выбран вручную; ожидание следующей проверки"}
	delete(groupControl.errors, id)
	groupControl.Unlock()
	return nil
}

type GroupCheckResult struct {
	State   string
	Message string
	Samples []GroupSample
	Policy  string
	Checked string
}

var groupChecks = struct {
	sync.Mutex
	results map[string]GroupCheckResult
}{results: map[string]GroupCheckResult{}}

func groupCheckStatus(id string) GroupCheckResult {
	groupChecks.Lock()
	defer groupChecks.Unlock()
	return groupChecks.results[id]
}
func setGroupCheck(id string, result GroupCheckResult) {
	groupChecks.Lock()
	defer groupChecks.Unlock()
	groupChecks.results[id] = result
}
func startGroupCheck(id string, requests ...string) error {
	g, ok := groupByID(id)
	if !ok {
		return fmt.Errorf("Группа не найдена")
	}
	if !probeLock.TryLock() {
		return fmt.Errorf("Другая проверка подключения уже выполняется; дождитесь её завершения")
	}
	raws := map[string]string{}
	for _, node := range g.Nodes {
		if nodeDisabled(node) {
			continue
		}
		var raw string
		if e := db.QueryRow("SELECT uri FROM nodes WHERE id=?", node).Scan(&raw); e != nil {
			probeLock.Unlock()
			return fmt.Errorf("Участник группы отсутствует")
		}
		raws[node] = raw
	}
	current := readGroupStatus(id).Tag
	runID := ""
	if len(requests) > 0 && len(requests[0]) <= 128 {
		runID = requests[0]
	}
	groupControl.Lock()
	state := groupControl.states[id]
	groupControl.Unlock()
	if state.Current == "" {
		state.Current = current
	}
	setGroupCheck(id, GroupCheckResult{State: "running", Message: "Проверяются участники в отдельном Xray; рабочий трафик не переключается"})
	go func() {
		defer probeLock.Unlock()
		result := GroupCheckResult{State: "running", Message: "Проверяются участники…"}
		all := map[string]GroupSample{}
		for _, node := range g.Nodes {
			if nodeDisabled(node) {
				continue
			}
			saveProbe(node, ProbeResult{State: "running", Mode: "real", RunID: runID})
			probe := probeNode(raws[node])
			probe.RunID = runID
			saveProbe(node, probe)
			tag := "auto-vpn-" + id + "-" + node + "-"
			sample := GroupSample{NodeID: node, Tag: tag, Alive: probe.State == "ok", DelayMS: probe.HTTPSMS, Checked: time.Now().Unix()}
			all[tag] = sample
			result.Samples = append(result.Samples, sample)
			result.Message = fmt.Sprintf("Проверено %d из %d", len(result.Samples), len(g.Nodes))
			setGroupCheck(id, result)
		}
		preview := g
		if preview.Mode == "fastest" {
			preview.Mode = "threshold"
			preview.ThresholdMS = 0
			preview.Failures = 1
			preview.Cooldown = 0
		}
		next := decideGroup(preview, all, state, time.Now())
		name := ""
		for _, node := range g.Nodes {
			if nodeDisabled(node) {
				continue
			}
			if next.Current == "auto-vpn-"+id+"-"+node+"-" {
				db.QueryRow("SELECT name FROM nodes WHERE id=?", node).Scan(&name)
			}
		}
		result.State = "done"
		result.Checked = time.Now().UTC().Format(time.RFC3339)
		result.Message = "Проверка завершена. "
		if name == "" {
			result.Message += "Все участники недоступны."
		} else {
			result.Message += "По сохранённой политике: " + name + ". " + next.Reason
		}
		result.Message += " Рабочий выбор не изменён. Это одна проверка; счётчик превышений и пауза учитываются в расчёте."
		setGroupCheck(id, result)
	}()
	return nil
}
