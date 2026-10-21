package main

import (
	"database/sql"
	"encoding/json"
)

type deviceRouteReader interface {
	QueryRow(string, ...any) *sql.Row
}

// A recycled numeric ID is never sufficient to establish device ownership.
func ownedDeviceRoute(q deviceRouteReader, id int64, ip string) (string, bool, error) {
	var target, disabled string
	err := q.QueryRow(`SELECT target,COALESCE((SELECT value FROM settings WHERE key='rule_disabled:' || rules.id),'0')
 FROM rules WHERE id=? AND kind='device' AND COALESCE((SELECT value FROM settings WHERE key='rule_source:' || rules.id),'')=?`, id, ip).Scan(&target, &disabled)
	return target, disabled == "1", err
}

// Run before deleted rule IDs can be reused, in the same transaction as the edit.
// Only the invalid association is cleared; rules and unrelated device metadata stay intact.
func detachInvalidDeviceRoutes(tx *sql.Tx) error {
	rows, err := tx.Query("SELECT key,value FROM settings WHERE key LIKE 'device:%'")
	if err != nil {
		return err
	}
	type record struct {
		key, raw string
		device   Device
	}
	records := []record{}
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.key, &r.raw); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal([]byte(r.raw), &r.device) == nil && r.device.RuleID != 0 {
			records = append(records, r)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range records {
		_, _, err = ownedDeviceRoute(tx, r.device.RuleID, r.device.IP)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal([]byte(r.raw), &fields); err != nil {
			return err
		}
		fields["RuleID"] = json.RawMessage("0")
		fields["Target"] = json.RawMessage(`""`)
		fields["TargetLabel"] = json.RawMessage(`""`)
		raw, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE settings SET value=? WHERE key=?", string(raw), r.key); err != nil {
			return err
		}
	}
	return nil
}

func repairDeviceRoutes() error {
	deviceLockRecord.Lock()
	defer deviceLockRecord.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = detachInvalidDeviceRoutes(tx); err != nil {
		return err
	}
	return tx.Commit()
}
