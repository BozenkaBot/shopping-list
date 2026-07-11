package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreListsItemsAreScopedAndPersisted(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "data", "shopping-list.json")
	s, err := New(filePath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	s.now = fixedClock()
	s.newID = sequenceID("home", "weekend", "item-home", "item-weekend")

	home, err := s.CreateList(CreateList{Name: "  Dom  "})
	if err != nil {
		t.Fatalf("CreateList(home) error = %v", err)
	}
	weekend, err := s.CreateList(CreateList{Name: "Weekend"})
	if err != nil {
		t.Fatalf("CreateList(weekend) error = %v", err)
	}

	homeItem, err := s.CreateItem(home.ID, CreateItem{Name: "  Chleb  ", Note: "  2 szt  "})
	if err != nil {
		t.Fatalf("CreateItem(home) error = %v", err)
	}
	weekendItem, err := s.CreateItem(weekend.ID, CreateItem{Name: "Kawa"})
	if err != nil {
		t.Fatalf("CreateItem(weekend) error = %v", err)
	}
	if homeItem.Name != "Chleb" || homeItem.Note != "2 szt" {
		t.Fatalf("home item = %+v", homeItem)
	}

	done := true
	name := "Chleb razowy"
	updated, err := s.UpdateItem(home.ID, homeItem.ID, UpdateItem{Name: &name, Completed: &done})
	if err != nil {
		t.Fatalf("UpdateItem(home) error = %v", err)
	}
	if updated.Name != name || !updated.Completed {
		t.Fatalf("updated item = %+v", updated)
	}

	homeItems, err := s.ListItems(home.ID)
	if err != nil {
		t.Fatalf("ListItems(home) error = %v", err)
	}
	weekendItems, err := s.ListItems(weekend.ID)
	if err != nil {
		t.Fatalf("ListItems(weekend) error = %v", err)
	}
	if len(homeItems) != 1 || homeItems[0].ID != homeItem.ID {
		t.Fatalf("home items = %+v", homeItems)
	}
	if len(weekendItems) != 1 || weekendItems[0].ID != weekendItem.ID {
		t.Fatalf("weekend items = %+v", weekendItems)
	}

	reloaded, err := New(filePath)
	if err != nil {
		t.Fatalf("reload New() error = %v", err)
	}
	lists := reloaded.Lists()
	if len(lists) != 3 {
		t.Fatalf("reloaded lists len = %d, want 3; lists = %+v", len(lists), lists)
	}
	if lists[1].Name != "Dom" || lists[1].TotalCount != 1 || lists[1].DoneCount != 1 {
		t.Fatalf("reloaded home summary = %+v", lists[1])
	}

	if err := reloaded.DeleteItem(weekend.ID, weekendItem.ID); err != nil {
		t.Fatalf("DeleteItem() error = %v", err)
	}
	weekendItems, err = reloaded.ListItems(weekend.ID)
	if err != nil {
		t.Fatalf("ListItems(weekend after delete) error = %v", err)
	}
	if len(weekendItems) != 0 {
		t.Fatalf("weekend items after delete = %+v", weekendItems)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var persisted DataFile
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("persisted JSON invalid: %v", err)
	}
	if len(persisted.Lists) != 3 || persisted.Lists[1].Items[0].Name != name {
		t.Fatalf("persisted data = %+v", persisted)
	}
}

func TestStoreValidationClearCompletedAndDeleteLastList(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "shopping-list.json")
	s, err := New(filePath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	s.now = fixedClock()
	s.newID = sequenceID("a", "b", "replacement")
	defaultID := s.DefaultListID()

	if _, err := s.CreateItem(defaultID, CreateItem{Name: " "}); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("CreateItem(empty) error = %v, want ErrEmptyName", err)
	}

	first, err := s.CreateItem(defaultID, CreateItem{Name: "Mleko"})
	if err != nil {
		t.Fatalf("CreateItem(first) error = %v", err)
	}
	second, err := s.CreateItem(defaultID, CreateItem{Name: "Jablka"})
	if err != nil {
		t.Fatalf("CreateItem(second) error = %v", err)
	}

	done := true
	if _, err := s.UpdateItem(defaultID, first.ID, UpdateItem{Completed: &done}); err != nil {
		t.Fatalf("UpdateItem(completed) error = %v", err)
	}
	removed, err := s.ClearCompletedItems(defaultID)
	if err != nil {
		t.Fatalf("ClearCompletedItems() error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("ClearCompletedItems() removed = %d, want 1", removed)
	}
	items, err := s.ListItems(defaultID)
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != second.ID {
		t.Fatalf("ListItems() after clear = %+v", items)
	}

	if err := s.DeleteList(defaultID); err != nil {
		t.Fatalf("DeleteList(last) error = %v", err)
	}
	lists := s.Lists()
	if len(lists) != 1 || lists[0].Name != DefaultListName || lists[0].TotalCount != 0 {
		t.Fatalf("Lists() after deleting last = %+v", lists)
	}
	if lists[0].ID == defaultID {
		t.Fatalf("replacement list reused deleted id %q", defaultID)
	}
}

func TestStoreMigratesLegacyItems(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "shopping-list.json")
	legacyItems := []Item{{
		ID:        "legacy-item",
		Name:      "Makaron",
		CreatedAt: fixedClock()(),
		UpdatedAt: fixedClock()(),
	}}
	data, err := json.Marshal(legacyItems)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	s, err := New(filePath)
	if err != nil {
		t.Fatalf("New(legacy) error = %v", err)
	}
	lists := s.Lists()
	if len(lists) != 1 || lists[0].Name != DefaultListName || lists[0].TotalCount != 1 {
		t.Fatalf("migrated lists = %+v", lists)
	}
	items, err := s.ListItems(lists[0].ID)
	if err != nil {
		t.Fatalf("ListItems(migrated) error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "legacy-item" {
		t.Fatalf("migrated items = %+v", items)
	}

	persisted, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var document DataFile
	if err := json.Unmarshal(persisted, &document); err != nil {
		t.Fatalf("persisted migrated JSON invalid: %v", err)
	}
	if len(document.Lists) != 1 || len(document.Lists[0].Items) != 1 {
		t.Fatalf("persisted migrated document = %+v", document)
	}
}

func fixedClock() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	}
}

func sequenceID(ids ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		if index >= len(ids) {
			return "extra", nil
		}
		id := ids[index]
		index++
		return id, nil
	}
}
