package main

import (
	"fmt"
	"strconv"
)

// Keep saved targets and group membership. Reserve deleted IDs so a future import
// cannot silently attach those references to a different server.
func deleteConnections(id string, source bool) error {
	query := "SELECT id FROM nodes WHERE id=?"
	if source {
		query = "SELECT id FROM nodes WHERE source_id=?"
	}
	rows, err := db.Query(query, id)
	if err != nil {
		return err
	}
	ids := []int{}
	for rows.Next() {
		var n int
		if err = rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !source && len(ids) == 0 {
		return fmt.Errorf("Подключение уже удалено")
	}
	selected := setting("selected_node")
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, n := range ids {
		key := strconv.Itoa(n)
		if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES(?, '1') ON CONFLICT(key) DO UPDATE SET value='1'", "node_deleted:"+key); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES('node_id_floor',?) ON CONFLICT(key) DO UPDATE SET value=CAST(MAX(CAST(value AS INTEGER),CAST(excluded.value AS INTEGER)) AS TEXT)", key); err != nil {
			return err
		}
		if selected == key {
			if _, err = tx.Exec("UPDATE settings SET value='' WHERE key='selected_node'"); err != nil {
				return err
			}
		}
		if _, err = tx.Exec("DELETE FROM nodes WHERE id=?", n); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM settings WHERE key IN (?,?)", "node_probe:"+key, "node_order:"+key); err != nil {
			return err
		}
	}
	if source {
		result, e := tx.Exec("DELETE FROM sources WHERE id=?", id)
		if e != nil {
			return e
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return fmt.Errorf("Подписка уже удалена")
		}
		for _, prefix := range []string{"source_disabled:", "sub_info:", "sub_interval:", "sub_attempt:"} {
			if _, err = tx.Exec("DELETE FROM settings WHERE key=?", prefix+id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

const nextNodeID = "(SELECT MAX(COALESCE(MAX(id),0),CAST(COALESCE((SELECT value FROM settings WHERE key='node_id_floor'),'0') AS INTEGER))+1 FROM nodes)"
