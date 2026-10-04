package main

import (
	"fmt"
	"strconv"
	"strings"
)

func orderString(rules []Rule) string {
	ids := []string{}
	for _, r := range rules {
		ids = append(ids, strconv.Itoa(r.ID))
	}
	return strings.Join(ids, ",")
}
func reorderRules(action, id, order, before string) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	rows, e := tx.Query("SELECT id FROM rules ORDER BY priority,id")
	if e != nil {
		return e
	}
	current := []string{}
	for rows.Next() {
		var n int
		if e = rows.Scan(&n); e != nil {
			rows.Close()
			return e
		}
		current = append(current, strconv.Itoa(n))
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if before != strings.Join(current, ",") {
		return fmt.Errorf("Список правил изменился. Обновите страницу и повторите перемещение")
	}
	next := append([]string{}, current...)
	if action == "rule-order" {
		next = strings.Split(order, ",")
		if len(next) != len(current) {
			return fmt.Errorf("Некорректный список правил")
		}
		allowed := map[string]bool{}
		for _, s := range current {
			allowed[s] = true
		}
		for _, s := range next {
			if !allowed[s] {
				return fmt.Errorf("Некорректный список правил")
			}
			delete(allowed, s)
		}
	} else {
		found := false
		for i, s := range current {
			if s != id {
				continue
			}
			found = true
			j := i - 1
			if action == "rule-down" {
				j = i + 1
			}
			if j >= 0 && j < len(next) {
				next[i], next[j] = next[j], next[i]
			}
			break
		}
		if !found {
			return fmt.Errorf("Правило не найдено")
		}
	}
	for i, s := range next {
		if _, e = tx.Exec("UPDATE rules SET priority=? WHERE id=?", i, s); e != nil {
			return e
		}
	}
	return tx.Commit()
}
