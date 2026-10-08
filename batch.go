package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
)

type BatchStatus struct {
	SourceID        string
	Mode            string
	RunID           string
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
	runID := probeRunID(modes)
	batchMu.Lock()
	batchStatus = BatchStatus{SourceID: id, Mode: mode, RunID: runID, State: "running", Total: len(group)}
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
		jobs := make([]probeJob, 0, len(group))
		previous := map[string]ProbeResult{}
		for _, n := range group {
			if ctx.Err() != nil {
				return
			}
			id := strconv.Itoa(n.ID)
			var raw string
			if db.QueryRow("SELECT uri FROM nodes WHERE id=?", id).Scan(&raw) != nil {
				batchMu.Lock()
				batchStatus.Done++
				batchMu.Unlock()
				continue
			}
			previous[id] = n.Probe
			jobs = append(jobs, probeJob{ID: id, Raw: raw})
		}
		runProbeJobs(ctx, jobs, mode, func(job probeJob, result ProbeResult) {
			if ctx.Err() != nil {
				return
			}
			result.RunID = runID
			saveProbe(job.ID, result)
			batchMu.Lock()
			batchStatus.Done++
			if result.State == "ok" {
				batchStatus.OK++
			}
			batchMu.Unlock()
		})
		if ctx.Err() != nil {
			// Restore only uncompleted jobs; never leave a cancelled spinner in storage.
			for _, job := range jobs {
				var current ProbeResult
				json.Unmarshal([]byte(setting("node_probe:"+job.ID)), &current)
				if current.RunID != runID || current.State == "running" {
					saveProbe(job.ID, previous[job.ID])
				}
			}
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
