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
	// Legacy native groups have no policy snapshot; fastest is already safe.
	if len(applied) == 0 {
		for id, g := range want {
			if g.Mode == "fastest" {
				delete(want, id)
			}
		}
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
	Current  string
	Bad      int
	Checked  int64
	Switched time.Time
	Reason   string
}

func decideGroup(g BalanceGroup, all map[string]GroupSample, state groupDecision, now time.Time) groupDecision {
	prefix := "auto-vpn-" + g.ID + "-"
	current, exists := all[state.Current]
	best := GroupSample{}
	fresh := func(s GroupSample) bool {
		return s.Alive && s.Checked > 0 && now.Unix()-s.Checked <= int64(g.Interval*3+15)
	}
	for _, id := range g.Nodes {
		s := all[prefix+id+"-"]
		if fresh(s) && (best.Tag == "" || s.DelayMS < best.DelayMS) {
			best = s
		}
	}
	choose := func(tag, reason string) groupDecision {
		if state.Current != tag {
			state.Current = tag
			state.Switched = now
		}
		state.Bad = 0
		state.Checked = 0
		state.Reason = reason
		return state
	}
	if best.Tag == "" {
		return choose("block", "Все участники недоступны или проверки устарели")
	}
	if !exists || !fresh(current) || !slices.Contains(g.Nodes, strings.TrimSuffix(strings.TrimPrefix(state.Current, prefix), "-")) {
		return choose(best.Tag, "Выбор доступного узла после старта или отказа")
	}
	if g.Mode == "failover" {
		state.Reason = "Узел доступен; переключение только при отказе"
		return state
	}
	if current.Checked != state.Checked {
		state.Checked = current.Checked
		if current.DelayMS > int64(g.ThresholdMS) {
			state.Bad++
		} else {
			state.Bad = 0
		}
	}
	if current.DelayMS <= int64(g.ThresholdMS) {
		state.Reason = fmt.Sprintf("Задержка не выше порога %d мс", g.ThresholdMS)
		return state
	}
	if state.Bad < g.Failures {
		state.Reason = fmt.Sprintf("Превышений подряд: %d из %d", state.Bad, g.Failures)
		return state
	}
	if now.Sub(state.Switched) < time.Duration(g.Cooldown)*time.Second {
		state.Reason = "Ожидание паузы между переключениями"
		return state
	}
	if best.Tag == state.Current || best.DelayMS >= current.DelayMS {
		state.Reason = "Порог превышен; более быстрого доступного узла нет"
		return state
	}
	return choose(best.Tag, fmt.Sprintf("Порог %d мс превышен; выбран более быстрый узел", g.ThresholdMS))
}

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
		snapshot, _ := json.Marshal(runtime.Groups)
		key := runtime.ConfigHash + string(snapshot)
		groupControl.Lock()
		if hash != key {
			hash = key
			groupControl.states = map[string]groupDecision{}
			groupControl.errors = map[string]string{}
		}
		groupControl.Unlock()
		for _, g := range runtime.Groups {
			normalizeGroup(&g)
			if g.Mode == "fastest" {
				continue
			}
			if validateGroupPolicy(g) != nil {
				continue
			}
			groupControl.Lock()
			old := groupControl.states[g.ID]
			groupControl.Unlock()
			if old.Current == "" {
				old.Current = readGroupStatus(g.ID).Tag
			}
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
				s.Policy = fmt.Sprintf("По порогу: %d мс, превышений %d, пауза %d с", applied.ThresholdMS, applied.Failures, applied.Cooldown)
			case "failover":
				s.Policy = "Только при отказе"
			default:
				s.Policy = "Самый быстрый"
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
