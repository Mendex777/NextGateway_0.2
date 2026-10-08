package main

import (
	"context"
	"fmt"
	"strconv"
	"sync"
)

type BatchStatus struct {
	SourceID        string
	Mode            string
	State           string
	Done, Total, OK int
}

var batchMu sync.Mutex
var batchStatus BatchStatus
var batchCancel context.CancelFunc

func startBatch(id string, modes ...string) error {
	mode, err := probeMode(modes)
	if err != nil {
		return err
	}
	var exists int
	if id != "" && db.QueryRow("SELECT id FROM sources WHERE id=?", id).Scan(&exists) != nil {
		return fmt.Errorf("Подписка не найдена")
	}
	nodes := allNodes()
	var group []Node
	for _, n := range nodes {
		if id == "" || strconv.Itoa(n.SourceID) == id {
			group = append(group, n)
		}
	}
	if len(group) == 0 {
		return fmt.Errorf("В подписке нет подключений")
	}
	if !probeLock.TryLock() {
		return fmt.Errorf("Проверка уже выполняется")
	}
	ctx, cancel := context.WithCancel(context.Background())
	batchMu.Lock()
	batchStatus = BatchStatus{SourceID: id, Mode: mode, State: "running", Total: len(group)}
	batchCancel = cancel
	batchMu.Unlock()
	go func() {
		defer probeLock.Unlock()
		defer cancel()
		defer func() {
			batchMu.Lock()
			if ctx.Err() != nil {
				batchStatus.State = "cancelled"
			} else {
				batchStatus.State = "done"
			}
			batchCancel = nil
			batchMu.Unlock()
		}()
		for _, n := range group {
			if ctx.Err() != nil {
				return
			}
			nodeID := strconv.Itoa(n.ID)
			var raw string
			if db.QueryRow("SELECT uri FROM nodes WHERE id=?", nodeID).Scan(&raw) != nil {
				batchMu.Lock()
				batchStatus.Done++
				batchMu.Unlock()
				continue
			}
			previous := n.Probe
			saveProbe(nodeID, ProbeResult{State: "running", Message: "Проверяется…"})
			result := probeNodeMode(ctx, raw, mode)
			if ctx.Err() != nil {
				saveProbe(nodeID, previous)
				return
			}
			saveProbe(nodeID, result)
			batchMu.Lock()
			batchStatus.Done++
			if result.State == "ok" {
				batchStatus.OK++
			}
			batchMu.Unlock()
		}
	}()
	return nil
}
func cancelBatch() {
	batchMu.Lock()
	defer batchMu.Unlock()
	if batchCancel != nil {
		batchCancel()
	}
}
func probeStatus() any {
	batchMu.Lock()
	status := batchStatus
	batchMu.Unlock()
	results := map[int]ProbeResult{}
	for _, n := range allNodes() {
		results[n.ID] = n.Probe
	}
	return struct {
		Batch BatchStatus
		Nodes map[int]ProbeResult
	}{status, results}
}
