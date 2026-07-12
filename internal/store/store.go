package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const DefaultListName = "Lista zakupów"

var (
	ErrNotFound     = errors.New("item not found")
	ErrListNotFound = errors.New("list not found")
	ErrEmptyName    = errors.New("name cannot be empty")
	ErrBadPatch     = errors.New("patch does not contain editable fields")
	ErrBadItemID    = errors.New("item id cannot be empty")
	ErrBadListID    = errors.New("list id cannot be empty")
	ErrBadDataDir   = errors.New("data file path must include a directory")
)

type ShoppingList struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Items     []Item    `json:"items"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ListSummary struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	TotalCount int       `json:"totalCount"`
	OpenCount  int       `json:"openCount"`
	DoneCount  int       `json:"doneCount"`
	Version    uint64    `json:"version"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type ItemsSnapshot struct {
	Items   []Item `json:"items"`
	Version uint64 `json:"version"`
}

type Item struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Note      string    `json:"note,omitempty"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CreateList struct {
	Name string `json:"name"`
}

type UpdateList struct {
	Name *string `json:"name"`
}

type CreateItem struct {
	Name string `json:"name"`
	Note string `json:"note"`
}

type UpdateItem struct {
	Name      *string `json:"name"`
	Note      *string `json:"note"`
	Completed *bool   `json:"completed"`
}

type Event struct {
	ID            uint64          `json:"id"`
	AggregateType string          `json:"aggregateType"`
	AggregateID   string          `json:"aggregateId"`
	ListID        string          `json:"listId,omitempty"`
	Type          string          `json:"type"`
	Payload       json.RawMessage `json:"payload"`
	CreatedAt     time.Time       `json:"createdAt"`
}

type Store struct {
	mu      sync.Mutex
	db      *sql.DB
	dbPath  string
	waiters map[chan struct{}]struct{}
	now     func() time.Time
	newID   func() (string, error)
}

func New(dbPath string) (*Store, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, ErrBadDataDir
	}
	dir := filepath.Dir(dbPath)
	if dir == "" {
		return nil, ErrBadDataDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}
	db.SetMaxOpenConns(1)

	s := &Store{
		db:      db,
		dbPath:  dbPath,
		waiters: make(map[chan struct{}]struct{}),
		now:     func() time.Time { return time.Now().UTC() },
		newID:   randomID,
	}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Lists() []ListSummary {
	s.mu.Lock()
	defer s.mu.Unlock()

	lists, err := s.listsLocked()
	if err != nil {
		return []ListSummary{}
	}
	return lists
}

func (s *Store) CreateList(input CreateList) (ListSummary, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return ListSummary{}, ErrEmptyName
	}
	id, err := s.newID()
	if err != nil {
		return ListSummary{}, fmt.Errorf("create list id: %w", err)
	}
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return ListSummary{}, err
	}
	defer tx.Rollback()

	version, err := s.appendEventLocked(tx, "list", id, "", "list_created", map[string]any{
		"id": id, "name": name,
	}, now)
	if err != nil {
		return ListSummary{}, err
	}
	if _, err := tx.Exec(`INSERT INTO lists (id, name, deleted, created_at, updated_at, version) VALUES (?, ?, 0, ?, ?, ?)`, id, name, formatTime(now), formatTime(now), version); err != nil {
		return ListSummary{}, fmt.Errorf("insert list projection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ListSummary{}, err
	}
	s.notifyLocked()

	return s.listSummaryLocked(id)
}

func (s *Store) UpdateList(id string, patch UpdateList) (ListSummary, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ListSummary{}, ErrBadListID
	}
	if patch.Name == nil {
		return ListSummary{}, ErrBadPatch
	}
	name := strings.TrimSpace(*patch.Name)
	if name == "" {
		return ListSummary{}, ErrEmptyName
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireListLocked(id); err != nil {
		return ListSummary{}, err
	}
	now := s.now()
	tx, err := s.db.Begin()
	if err != nil {
		return ListSummary{}, err
	}
	defer tx.Rollback()

	version, err := s.appendEventLocked(tx, "list", id, "", "list_renamed", map[string]any{"name": name}, now)
	if err != nil {
		return ListSummary{}, err
	}
	if _, err := tx.Exec(`UPDATE lists SET name = ?, updated_at = ?, version = ? WHERE id = ? AND deleted = 0`, name, formatTime(now), version, id); err != nil {
		return ListSummary{}, fmt.Errorf("update list projection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ListSummary{}, err
	}
	s.notifyLocked()

	return s.listSummaryLocked(id)
}

func (s *Store) DeleteList(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrBadListID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireListLocked(id); err != nil {
		return err
	}
	now := s.now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	version, err := s.appendEventLocked(tx, "list", id, "", "list_deleted", map[string]any{"id": id}, now)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE lists SET deleted = 1, updated_at = ?, version = ? WHERE id = ? AND deleted = 0`, formatTime(now), version, id); err != nil {
		return fmt.Errorf("delete list projection: %w", err)
	}
	active, err := countActiveLists(tx)
	if err != nil {
		return err
	}
	if active == 0 {
		if _, err := s.createDefaultListLocked(tx, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notifyLocked()
	return nil
}

func (s *Store) ListItemsSnapshot(listID string) (ItemsSnapshot, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return ItemsSnapshot{}, ErrBadListID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireListLocked(listID); err != nil {
		return ItemsSnapshot{}, err
	}
	items, err := s.listItemsLocked(listID)
	if err != nil {
		return ItemsSnapshot{}, err
	}
	version, err := s.listVersionLocked(listID)
	if err != nil {
		return ItemsSnapshot{}, err
	}
	return ItemsSnapshot{Items: items, Version: version}, nil
}

func (s *Store) ListItems(listID string) ([]Item, error) {
	snapshot, err := s.ListItemsSnapshot(listID)
	if err != nil {
		return nil, err
	}
	return snapshot.Items, nil
}

func (s *Store) CreateItem(listID string, input CreateItem) (Item, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return Item{}, ErrBadListID
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Item{}, ErrEmptyName
	}
	id, err := s.newID()
	if err != nil {
		return Item{}, fmt.Errorf("create item id: %w", err)
	}
	note := strings.TrimSpace(input.Note)
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireListLocked(listID); err != nil {
		return Item{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()

	version, err := s.appendEventLocked(tx, "item", id, listID, "item_added", map[string]any{
		"id": id, "listId": listID, "name": name, "note": note,
	}, now)
	if err != nil {
		return Item{}, err
	}
	if _, err := tx.Exec(`INSERT INTO items (id, list_id, name, note, completed, deleted, created_at, updated_at, version) VALUES (?, ?, ?, ?, 0, 0, ?, ?, ?)`, id, listID, name, note, formatTime(now), formatTime(now), version); err != nil {
		return Item{}, fmt.Errorf("insert item projection: %w", err)
	}
	if _, err := tx.Exec(`UPDATE lists SET updated_at = ?, version = ? WHERE id = ?`, formatTime(now), version, listID); err != nil {
		return Item{}, fmt.Errorf("touch list projection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Item{}, err
	}
	s.notifyLocked()

	return Item{ID: id, Name: name, Note: note, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) UpdateItem(listID, itemID string, patch UpdateItem) (Item, error) {
	listID = strings.TrimSpace(listID)
	itemID = strings.TrimSpace(itemID)
	if listID == "" {
		return Item{}, ErrBadListID
	}
	if itemID == "" {
		return Item{}, ErrBadItemID
	}
	if patch.Name == nil && patch.Note == nil && patch.Completed == nil {
		return Item{}, ErrBadPatch
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireListLocked(listID); err != nil {
		return Item{}, err
	}
	current, err := s.itemLocked(listID, itemID)
	if err != nil {
		return Item{}, err
	}
	next := current
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" {
			return Item{}, ErrEmptyName
		}
		next.Name = name
	}
	if patch.Note != nil {
		next.Note = strings.TrimSpace(*patch.Note)
	}
	if patch.Completed != nil {
		next.Completed = *patch.Completed
	}
	next.UpdatedAt = s.now()

	eventType := "item_updated"
	if patch.Completed != nil && patch.Name == nil && patch.Note == nil {
		eventType = "item_completed_changed"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()

	version, err := s.appendEventLocked(tx, "item", itemID, listID, eventType, map[string]any{
		"id": itemID, "listId": listID, "name": next.Name, "note": next.Note, "completed": next.Completed,
	}, next.UpdatedAt)
	if err != nil {
		return Item{}, err
	}
	if _, err := tx.Exec(`UPDATE items SET name = ?, note = ?, completed = ?, updated_at = ?, version = ? WHERE id = ? AND list_id = ? AND deleted = 0`, next.Name, next.Note, boolInt(next.Completed), formatTime(next.UpdatedAt), version, itemID, listID); err != nil {
		return Item{}, fmt.Errorf("update item projection: %w", err)
	}
	if _, err := tx.Exec(`UPDATE lists SET updated_at = ?, version = ? WHERE id = ?`, formatTime(next.UpdatedAt), version, listID); err != nil {
		return Item{}, fmt.Errorf("touch list projection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Item{}, err
	}
	s.notifyLocked()

	return next, nil
}

func (s *Store) DeleteItem(listID, itemID string) error {
	listID = strings.TrimSpace(listID)
	itemID = strings.TrimSpace(itemID)
	if listID == "" {
		return ErrBadListID
	}
	if itemID == "" {
		return ErrBadItemID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireListLocked(listID); err != nil {
		return err
	}
	if _, err := s.itemLocked(listID, itemID); err != nil {
		return err
	}
	now := s.now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	version, err := s.appendEventLocked(tx, "item", itemID, listID, "item_deleted", map[string]any{"id": itemID, "listId": listID}, now)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE items SET deleted = 1, updated_at = ?, version = ? WHERE id = ? AND list_id = ? AND deleted = 0`, formatTime(now), version, itemID, listID); err != nil {
		return fmt.Errorf("delete item projection: %w", err)
	}
	if _, err := tx.Exec(`UPDATE lists SET updated_at = ?, version = ? WHERE id = ?`, formatTime(now), version, listID); err != nil {
		return fmt.Errorf("touch list projection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notifyLocked()
	return nil
}

func (s *Store) ClearCompletedItems(listID string) (int, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return 0, ErrBadListID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireListLocked(listID); err != nil {
		return 0, err
	}
	var removed int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items WHERE list_id = ? AND completed = 1 AND deleted = 0`, listID).Scan(&removed); err != nil {
		return 0, err
	}
	if removed == 0 {
		return 0, nil
	}
	now := s.now()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	version, err := s.appendEventLocked(tx, "list", listID, listID, "completed_items_cleared", map[string]any{"listId": listID, "removed": removed}, now)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE items SET deleted = 1, updated_at = ?, version = ? WHERE list_id = ? AND completed = 1 AND deleted = 0`, formatTime(now), version, listID); err != nil {
		return 0, fmt.Errorf("clear completed projection: %w", err)
	}
	if _, err := tx.Exec(`UPDATE lists SET updated_at = ?, version = ? WHERE id = ?`, formatTime(now), version, listID); err != nil {
		return 0, fmt.Errorf("touch list projection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.notifyLocked()
	return removed, nil
}

func (s *Store) WaitForVersion(ctx context.Context, listID string, since uint64, timeout time.Duration) (uint64, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return 0, ErrBadListID
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		s.mu.Lock()
		if err := s.requireListLocked(listID); err != nil {
			s.mu.Unlock()
			return 0, err
		}
		version, err := s.listVersionLocked(listID)
		if err != nil {
			s.mu.Unlock()
			return 0, err
		}
		if version > since {
			s.mu.Unlock()
			return version, nil
		}
		ch := make(chan struct{})
		s.waiters[ch] = struct{}{}
		s.mu.Unlock()

		select {
		case <-ch:
		case <-deadline.C:
			s.mu.Lock()
			delete(s.waiters, ch)
			s.mu.Unlock()
			return version, nil
		case <-ctx.Done():
			s.mu.Lock()
			delete(s.waiters, ch)
			s.mu.Unlock()
			return 0, ctx.Err()
		}
	}
}

func (s *Store) DefaultListID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := s.defaultListIDLocked()
	if err != nil {
		return ""
	}
	return id
}

func (s *Store) EventsSince(since uint64) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id, aggregate_type, aggregate_id, COALESCE(list_id, ''), type, payload, created_at FROM events WHERE id > ? ORDER BY id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		var payload, createdAt string
		if err := rows.Scan(&event.ID, &event.AggregateType, &event.AggregateID, &event.ListID, &event.Type, &payload, &createdAt); err != nil {
			return nil, err
		}
		event.Payload = json.RawMessage(payload)
		event.CreatedAt = parseTime(createdAt)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) init() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec(`PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000; PRAGMA foreign_keys = ON;`); err != nil {
		return fmt.Errorf("configure sqlite: %w", err)
	}
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	aggregate_type TEXT NOT NULL,
	aggregate_id TEXT NOT NULL,
	list_id TEXT,
	type TEXT NOT NULL,
	payload TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS lists (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	deleted INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	version INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS items (
	id TEXT PRIMARY KEY,
	list_id TEXT NOT NULL REFERENCES lists(id),
	name TEXT NOT NULL,
	note TEXT NOT NULL DEFAULT '',
	completed INTEGER NOT NULL DEFAULT 0,
	deleted INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	version INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_list_id ON events(list_id, id);
CREATE INDEX IF NOT EXISTS idx_events_aggregate ON events(aggregate_type, aggregate_id, id);
CREATE INDEX IF NOT EXISTS idx_items_list_active ON items(list_id, deleted, completed);
`); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	var active int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM lists WHERE deleted = 0`).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := s.createDefaultListLocked(tx, s.now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) createDefaultListLocked(tx *sql.Tx, now time.Time) (string, error) {
	id, err := s.newID()
	if err != nil {
		return "", fmt.Errorf("create default list id: %w", err)
	}
	version, err := s.appendEventLocked(tx, "list", id, "", "list_created", map[string]any{
		"id": id, "name": DefaultListName, "default": true,
	}, now)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO lists (id, name, deleted, created_at, updated_at, version) VALUES (?, ?, 0, ?, ?, ?)`, id, DefaultListName, formatTime(now), formatTime(now), version); err != nil {
		return "", fmt.Errorf("insert default list projection: %w", err)
	}
	return id, nil
}

func (s *Store) appendEventLocked(tx *sql.Tx, aggregateType, aggregateID, listID, eventType string, payload any, createdAt time.Time) (uint64, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("encode event payload: %w", err)
	}
	var nullableListID any
	if listID != "" {
		nullableListID = listID
	}
	result, err := tx.Exec(`INSERT INTO events (aggregate_type, aggregate_id, list_id, type, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)`, aggregateType, aggregateID, nullableListID, eventType, string(data), formatTime(createdAt))
	if err != nil {
		return 0, fmt.Errorf("append event %s: %w", eventType, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read event id: %w", err)
	}
	return uint64(id), nil
}

func (s *Store) listsLocked() ([]ListSummary, error) {
	rows, err := s.db.Query(`
SELECT l.id, l.name, l.created_at, l.updated_at,
	COUNT(i.id) AS total_count,
	COALESCE(SUM(CASE WHEN i.completed = 0 THEN 1 ELSE 0 END), 0) AS open_count,
	COALESCE(SUM(CASE WHEN i.completed = 1 THEN 1 ELSE 0 END), 0) AS done_count,
	COALESCE((SELECT MAX(e.id) FROM events e WHERE e.list_id = l.id OR e.aggregate_id = l.id OR e.aggregate_type = 'list'), l.version) AS version
FROM lists l
LEFT JOIN items i ON i.list_id = l.id AND i.deleted = 0
WHERE l.deleted = 0
GROUP BY l.id
ORDER BY (SELECT MIN(e.id) FROM events e WHERE e.aggregate_type = 'list' AND e.aggregate_id = l.id AND e.type = 'list_created'), l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []ListSummary
	for rows.Next() {
		var list ListSummary
		var createdAt, updatedAt string
		if err := rows.Scan(&list.ID, &list.Name, &createdAt, &updatedAt, &list.TotalCount, &list.OpenCount, &list.DoneCount, &list.Version); err != nil {
			return nil, err
		}
		list.CreatedAt = parseTime(createdAt)
		list.UpdatedAt = parseTime(updatedAt)
		lists = append(lists, list)
	}
	return lists, rows.Err()
}

func (s *Store) listSummaryLocked(id string) (ListSummary, error) {
	var list ListSummary
	var createdAt, updatedAt string
	err := s.db.QueryRow(`
SELECT l.id, l.name, l.created_at, l.updated_at,
	COUNT(i.id) AS total_count,
	COALESCE(SUM(CASE WHEN i.completed = 0 THEN 1 ELSE 0 END), 0) AS open_count,
	COALESCE(SUM(CASE WHEN i.completed = 1 THEN 1 ELSE 0 END), 0) AS done_count,
	COALESCE((SELECT MAX(e.id) FROM events e WHERE e.list_id = l.id OR e.aggregate_id = l.id OR e.aggregate_type = 'list'), l.version) AS version
FROM lists l
LEFT JOIN items i ON i.list_id = l.id AND i.deleted = 0
WHERE l.id = ? AND l.deleted = 0
GROUP BY l.id`, id).Scan(&list.ID, &list.Name, &createdAt, &updatedAt, &list.TotalCount, &list.OpenCount, &list.DoneCount, &list.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return ListSummary{}, ErrListNotFound
	}
	if err != nil {
		return ListSummary{}, err
	}
	list.CreatedAt = parseTime(createdAt)
	list.UpdatedAt = parseTime(updatedAt)
	return list, nil
}

func (s *Store) listItemsLocked(listID string) ([]Item, error) {
	rows, err := s.db.Query(`SELECT id, name, note, completed, created_at, updated_at FROM items WHERE list_id = ? AND deleted = 0 ORDER BY (SELECT MIN(e.id) FROM events e WHERE e.aggregate_type = 'item' AND e.aggregate_id = items.id AND e.type = 'item_added'), id`, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		var completed int
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.Name, &item.Note, &completed, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.Completed = completed != 0
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) itemLocked(listID, itemID string) (Item, error) {
	var item Item
	var completed int
	var createdAt, updatedAt string
	err := s.db.QueryRow(`SELECT id, name, note, completed, created_at, updated_at FROM items WHERE list_id = ? AND id = ? AND deleted = 0`, listID, itemID).Scan(&item.ID, &item.Name, &item.Note, &completed, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	item.Completed = completed != 0
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func (s *Store) requireListLocked(id string) error {
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM lists WHERE id = ? AND deleted = 0`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrListNotFound
	}
	return err
}

func (s *Store) defaultListIDLocked() (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM lists WHERE deleted = 0 ORDER BY (SELECT MIN(e.id) FROM events e WHERE e.aggregate_type = 'list' AND e.aggregate_id = lists.id AND e.type = 'list_created'), id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrListNotFound
	}
	return id, err
}

func (s *Store) listVersionLocked(listID string) (uint64, error) {
	var version sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(id) FROM events WHERE list_id = ? OR aggregate_id = ? OR aggregate_type = 'list'`, listID, listID).Scan(&version)
	if err != nil {
		return 0, err
	}
	if !version.Valid {
		return 0, nil
	}
	return uint64(version.Int64), nil
}

func (s *Store) notifyLocked() {
	for ch := range s.waiters {
		close(ch)
		delete(s.waiters, ch)
	}
}

func countActiveLists(tx *sql.Tx) (int, error) {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM lists WHERE deleted = 0`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
