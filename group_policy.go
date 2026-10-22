package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protowire"
)

func formInt(r *http.Request, key string, fallback int) int {
	if r.FormValue(key) == "" {
		return fallback
	}
	n, e := strconv.Atoi(r.FormValue(key))
	if e != nil {
		return -1
	}
	return n
}
func normalizeGroup(g *BalanceGroup) {
	if g.ReturnSuccesses == 0 {
		g.ReturnSuccesses = 20
	}
	if g.FastestPolicy == "" {
		g.FastestPolicy = "gain"
	}
	if g.GainMS == 0 {
		g.GainMS = 50
	}
	if g.Mode == "" {
		g.Mode = "fastest"
		g.Cooldown = 60
	}
	if g.ThresholdMS == 0 {
		g.ThresholdMS = 1000
	}
	if g.Failures == 0 {
		g.Failures = 2
	}
}
func validateGroupPolicy(g BalanceGroup) error {
	if g.ReturnSuccesses < 0 || g.ReturnSuccesses > 100 {
		return fmt.Errorf("Для возврата: от 1 до 100 успешных проверок")
	}
	if (g.FastestPolicy != "" && g.FastestPolicy != "gain" && g.FastestPolicy != "absolute") || g.GainMS < 0 || g.GainMS > 60000 {
		return fmt.Errorf("Некорректный режим сравнения задержки или выигрыш задержки")
	}
	if !slices.Contains([]string{"fastest", "threshold", "failover"}, g.Mode) || g.ThresholdMS < 50 || g.ThresholdMS > 60000 || g.Failures < 1 || g.Failures > 10 || g.Cooldown < 0 || g.Cooldown > 3600 {
		return fmt.Errorf("Порог: 50–60000 мс; превышений подряд: 1–10; пауза: 0–3600 с")
	}
	return nil
}
func groupPolicyPending(c map[string]any, applied []BalanceGroup) bool {
	want := map[string]BalanceGroup{}
	have := map[string]BalanceGroup{}
	if bal, ok := c["routing"].(map[string]any)["balancers"].([]any); ok {
		for _, b := range bal {
			tag := b.(map[string]any)["tag"].(string)
			if g, ok := groupByID(strings.TrimPrefix(tag, "group-")); ok {
				g.Members = nil
				want[g.ID] = g
			}
		}
	}
	for _, g := range applied {
		normalizeGroup(&g)
		g.Members = nil
		have[g.ID] = g
	}
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(have)
	return string(a) != string(b)
}

type GroupSample struct {
	NodeID  string
	Tag     string
	Alive   bool
	DelayMS int64
	Checked int64
	Error   string
	Active  bool
}
type rawProtoCodec struct{}

func (rawProtoCodec) Name() string { return "proto" }
func (rawProtoCodec) Marshal(v any) ([]byte, error) {
	p, ok := v.(*[]byte)
	if !ok {
		return nil, fmt.Errorf("invalid protobuf input")
	}
	return *p, nil
}
func (rawProtoCodec) Unmarshal(b []byte, v any) error {
	p, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("invalid protobuf output")
	}
	*p = append((*p)[:0], b...)
	return nil
}

var groupAPIAddress = "127.0.0.1:10085"

func coreRPC(method string, request []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, e := grpc.DialContext(ctx, groupAPIAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if e != nil {
		return nil, e
	}
	defer conn.Close()
	var response []byte
	e = conn.Invoke(ctx, method, &request, &response, grpc.ForceCodec(rawProtoCodec{}), grpc.MaxCallRecvMsgSize(2*1024*1024))
	return response, e
}

// Wire fields follow the pinned Xray v26.3.27 observatory protobuf schema.
func protoFields(b []byte, visit func(protowire.Number, uint64, []byte)) error {
	for len(b) > 0 {
		n, t, k := protowire.ConsumeTag(b)
		if k < 0 {
			return fmt.Errorf("invalid protobuf tag")
		}
		b = b[k:]
		var v uint64
		var data []byte
		switch t {
		case protowire.VarintType:
			v, k = protowire.ConsumeVarint(b)
		case protowire.BytesType:
			data, k = protowire.ConsumeBytes(b)
		default:
			k = protowire.ConsumeFieldValue(n, t, b)
		}
		if k < 0 {
			return fmt.Errorf("invalid protobuf value")
		}
		visit(n, v, data)
		b = b[k:]
	}
	return nil
}
func parseObservations(raw []byte) (map[string]GroupSample, error) {
	out := map[string]GroupSample{}
	var nestedErr error
	e := protoFields(raw, func(n protowire.Number, _ uint64, result []byte) {
		if n != 1 {
			return
		}
		resultErr := protoFields(result, func(n protowire.Number, _ uint64, item []byte) {
			if n != 1 {
				return
			}
			var s GroupSample
			e := protoFields(item, func(n protowire.Number, v uint64, data []byte) {
				switch n {
				case 1:
					s.Alive = v != 0
				case 2:
					s.DelayMS = int64(v)
				case 3:
					s.Error = string(data)
				case 4:
					s.Tag = string(data)
				case 6:
					s.Checked = int64(v)
				}
			})
			if e != nil {
				nestedErr = e
			}
			if s.Tag != "" {
				out[s.Tag] = s
			}
		})
		if resultErr != nil {
			nestedErr = resultErr
		}
	})
	if e != nil {
		return nil, e
	}
	return out, nestedErr
}
func observations() (map[string]GroupSample, error) {
	raw, e := coreRPC("/xray.core.app.observatory.command.ObservatoryService/GetOutboundStatus", nil)
	if e != nil {
		return nil, e
	}
	return parseObservations(raw)
}
func overrideGroup(id, target string) error {
	b := protowire.AppendTag(nil, 1, protowire.BytesType)
	b = protowire.AppendString(b, "group-"+id)
	b = protowire.AppendTag(b, 2, protowire.BytesType)
	b = protowire.AppendString(b, target)
	_, e := coreRPC("/xray.app.router.command.RoutingService/OverrideBalancerTarget", b)
	return e
}

type groupDecision struct {
	ReturnTarget     string
	ReturnGood       int
	ReturnChecked    int64
	Candidate        string
	CandidateChecked int64
	Current          string
	Bad              int
	Checked          int64
	Switched         time.Time
	Reason           string
	Manual           bool
	ManualChecked    int64
}

func decideGroup(g BalanceGroup, all map[string]GroupSample, state groupDecision, now time.Time) groupDecision {
	prefix := "auto-vpn-" + g.ID + "-"
	current, exists := all[state.Current]
	preferredTag := ""
	if g.Mode == "failover" && g.ReturnToPreferred && slices.Contains(g.Nodes, g.PreferredNode) {
		preferredTag = prefix + g.PreferredNode + "-"
	}
	returnChecks := g.ReturnSuccesses
	if returnChecks == 0 {
		returnChecks = 20
	}
	if state.ReturnTarget != preferredTag {
		state.ReturnTarget = preferredTag
		state.ReturnGood = 0
		state.ReturnChecked = 0
	}
	preferred := all[preferredTag]
	preferredFresh := preferredTag != "" && preferred.Alive && preferred.Checked > 0 && now.Unix()-preferred.Checked <= int64(g.Interval*3+15)
	if preferredTag == "" || state.Current == preferredTag {
		state.ReturnGood = 0
		state.ReturnChecked = preferred.Checked
	} else if !preferredFresh {
		state.ReturnGood = 0
		state.ReturnChecked = preferred.Checked
	} else if preferred.Checked != state.ReturnChecked {
		state.ReturnChecked = preferred.Checked
		if state.ReturnGood < returnChecks {
			state.ReturnGood++
		}
	}
	best := GroupSample{}
	fresh := func(s GroupSample) bool {
		return s.Alive && s.Checked > 0 && now.Unix()-s.Checked <= int64(g.Interval*3+15)
	}
	for _, id := range g.Nodes {
		s := all[prefix+id+"-"]
		if s.Tag == preferredTag && state.Current != preferredTag && state.ReturnGood < returnChecks {
			continue
		}
		if fresh(s) && (best.Tag == "" || s.DelayMS < best.DelayMS) {
			best = s
		}
	}
	choose := func(tag, reason string) groupDecision {
		if state.Current != tag {
			state.Current = tag
			state.Switched = now
			state.ReturnGood = 0
			state.ReturnChecked = preferred.Checked
		}
		state.Bad = 0
		state.Checked = 0
		state.Candidate = ""
		state.CandidateChecked = 0
		state.Reason = reason
		return state
	}
	available := func(reason string) groupDecision {
		if best.Tag == "" {
			if preferredFresh && state.ReturnGood < returnChecks {
				return choose("block", "Ожидание подтверждённых проверок приоритетного узла")
			}
			return choose("block", "Все участники недоступны или проверки устарели")
		}
		return choose(best.Tag, reason)
	}
	// Missing/stale observations are not new failed checks and cannot be counted.
	if !exists || current.Checked <= 0 || now.Unix()-current.Checked > int64(g.Interval*3+15) || !slices.Contains(g.Nodes, strings.TrimSuffix(strings.TrimPrefix(state.Current, prefix), "-")) {
		return available("Выбор доступного узла после старта или устаревания проверок")
	}
	if preferredTag != "" && state.Current != preferredTag && preferredFresh && state.ReturnGood >= returnChecks && now.Sub(state.Switched) >= time.Duration(g.Cooldown)*time.Second {
		return choose(preferredTag, "Приоритетный узел восстановлен; успешные проверки подтверждены")
	}
	gainMode := g.Mode == "fastest" && g.FastestPolicy != "absolute"
	bad := !current.Alive
	if current.Alive && g.Mode != "failover" {
		if gainMode {
			bad = best.Tag != "" && best.Tag != state.Current && current.DelayMS > best.DelayMS && current.DelayMS-best.DelayMS >= int64(g.GainMS)
		} else {
			bad = current.DelayMS > int64(g.ThresholdMS)
		}
	}
	if gainMode && current.Alive {
		if best.Tag != state.Candidate {
			state.Bad = 0
			state.Candidate = best.Tag
			state.CandidateChecked = 0
		}
		if !bad {
			state.Bad = 0
			state.Checked = current.Checked
			state.CandidateChecked = best.Checked
		} else if current.Checked != state.Checked && best.Checked != state.CandidateChecked {
			state.Checked = current.Checked
			state.CandidateChecked = best.Checked
			state.Bad++
		}
	} else if current.Checked != state.Checked {
		state.Checked = current.Checked
		state.Candidate = ""
		state.CandidateChecked = 0
		if bad {
			state.Bad++
		} else {
			state.Bad = 0
		}
	}
	if !bad {
		switch {
		case g.Mode == "failover":
			state.Reason = "Узел доступен; переключение только при отказе"
			if preferredTag != "" && state.Current != preferredTag {
				if !preferredFresh {
					state.Reason = "Работаем на запасном узле; приоритетный недоступен"
				} else if state.ReturnGood < returnChecks {
					state.Reason = fmt.Sprintf("Возврат на приоритетный: успешных проверок %d из %d", state.ReturnGood, returnChecks)
				} else {
					state.Reason = "Приоритетный восстановлен; ожидание паузы перед возвратом"
				}
			}
		case gainMode:
			state.Reason = fmt.Sprintf("Нет другого узла быстрее на %d мс", g.GainMS)
		default:
			state.Reason = fmt.Sprintf("Задержка не выше порога %d мс", g.ThresholdMS)
		}
		return state
	}
	if state.Bad < g.Failures {
		label := "Неудачных проверок или превышений подряд"
		if g.Mode == "failover" || !current.Alive {
			label = "Неудачных проверок подряд"
		}
		if gainMode && current.Alive {
			label = "Подтверждений преимущества узла подряд"
		}
		state.Reason = fmt.Sprintf("%s: %d из %d", label, state.Bad, g.Failures)
		return state
	}
	if now.Sub(state.Switched) < time.Duration(g.Cooldown)*time.Second {
		state.Reason = "Ожидание паузы между переключениями"
		return state
	}
	if !current.Alive {
		return available("Подтверждён отказ узла; выбран доступный узел")
	}
	if best.Tag == state.Current || best.DelayMS >= current.DelayMS {
		state.Reason = "Порог превышен; более быстрого доступного узла нет"
		return state
	}
	if gainMode {
		return choose(best.Tag, fmt.Sprintf("Другой узел быстрее минимум на %d мс; проверки подтверждены", g.GainMS))
	}
	return choose(best.Tag, fmt.Sprintf("Порог %d мс превышен; выбран более быстрый узел", g.ThresholdMS))
}

var groupActionLock sync.Mutex

var groupControl = struct {
	sync.Mutex
	states map[string]groupDecision
	errors map[string]string
}{states: map[string]groupDecision{}, errors: map[string]string{}}

func groupWorker() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	hash := ""
	for range ticker.C {
		runtime := readRuntime()
		if runtime.State == "running" || !runtime.Balance {
			continue
		}
		all, e := observations()
		if e != nil {
			continue
		}
		observeMapProbes(all)
		snapshot, _ := json.Marshal(runtime.Groups)
		key := runtime.ConfigHash + string(snapshot)
		groupControl.Lock()
		if hash != "" && hash != key {
			groupControl.states = map[string]groupDecision{}
			groupControl.errors = map[string]string{}
		}
		groupControl.Unlock()
		hash = key
		for _, g := range runtime.Groups {
			normalizeGroup(&g)

			if validateGroupPolicy(g) != nil {
				continue
			}
			groupActionLock.Lock()
			groupControl.Lock()
			old := groupControl.states[g.ID]
			groupControl.Unlock()
			if old.Current == "" {
				status := readGroupStatus(g.ID)
				old.Current = status.Tag
				if g.Mode == "fastest" && status.Override {
					old.Manual = true
					old.ManualChecked = -1
				}
			}
			if old.Manual && all[old.Current].Checked == old.ManualChecked {
				groupActionLock.Unlock()
				continue
			}
			old.Manual = false
			g.PreferredNode = preferredGroupNode(g)
			next := decideGroup(g, all, old, time.Now())
			// Reassert the override: it may be lost on an independent core restart.
			e = overrideGroup(g.ID, next.Current)
			groupControl.Lock()
			if e != nil {
				groupControl.errors[g.ID] = "Не удалось управлять балансером через API"
			} else {
				groupControl.states[g.ID] = next
				delete(groupControl.errors, g.ID)
			}
			groupControl.Unlock()
			groupActionLock.Unlock()
		}
	}
}
func enrichGroupStatus(id string, s BalanceStatus) BalanceStatus {
	all, e := observations()
	if e != nil {
		s.Controller = "Задержки недоступны: примените конфигурацию с API наблюдателя"
		return s
	}
	g, ok := groupByID(id)
	if !ok {
		return s
	}
	s.PreferredNode = preferredGroupNode(g)
	if s.PreferredNode != "" {
		db.QueryRow("SELECT name FROM nodes WHERE id=?", s.PreferredNode).Scan(&s.PreferredName)
	}
	for _, node := range g.Nodes {
		tag := "auto-vpn-" + id + "-" + node + "-"
		sample := all[tag]
		sample.NodeID = node
		sample.Tag = tag
		sample.Active = tag == s.Tag
		s.Samples = append(s.Samples, sample)
	}
	for _, applied := range readRuntime().Groups {
		if applied.ID == id {
			normalizeGroup(&applied)
			switch applied.Mode {
			case "threshold":
				s.Policy = fmt.Sprintf("По порогу: %d мс, ошибок или превышений подряд %d, пауза %d с", applied.ThresholdMS, applied.Failures, applied.Cooldown)
			case "failover":
				s.Policy = fmt.Sprintf("Только при отказе: неудачных проверок %d, пауза %d с", applied.Failures, applied.Cooldown)
				if applied.ReturnToPreferred {
					s.Policy += fmt.Sprintf("; возврат после %d успешных проверок", applied.ReturnSuccesses)
				}
			default:
				if applied.FastestPolicy == "absolute" {
					s.Policy = fmt.Sprintf("Самый быстрый: порог %d мс, проверок %d, пауза %d с", applied.ThresholdMS, applied.Failures, applied.Cooldown)
				} else {
					s.Policy = fmt.Sprintf("Самый быстрый: другой быстрее на %d мс, проверок %d, пауза %d с", applied.GainMS, applied.Failures, applied.Cooldown)
				}
			}
		}
	}
	if s.Policy == "" {
		s.Policy = "Самый быстрый"
	}
	groupControl.Lock()
	s.Controller = groupControl.states[id].Reason
	if message := groupControl.errors[id]; message != "" {
		s.Controller = message
	}
	groupControl.Unlock()
	return s
}
