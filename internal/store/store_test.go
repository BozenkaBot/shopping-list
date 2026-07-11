package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreCreateUpdateDeleteAndPersist(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "data", "shopping-list.json")
	s, err := New(filePath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	s.now = fixedClock()
	s.newID = sequenceID("item-1")

	item, err := s.Create(CreateItem{Name: "  Chleb  ", Note: "  2 szt  "})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if item.Name != "Chleb" || item.Note != "2 szt" || item.Completed {
		t.Fatalf("Create() item = %+v", item)
	}

	done := true
	name := "Chleb razowy"
	updated, err := s.Update(item.ID, UpdateItem{Name: &name, Completed: &done})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != name || !updated.Completed {
		t.Fatalf("Update() item = %+v", updated)
	}

	reloaded, err := New(filePath)
	if err != nil {
		t.Fatalf("reload New() error = %v", err)
	}
	items := reloaded.List()
	if len(items) != 1 || items[0].Name != name || !items[0].Completed {
		t.Fatalf("reloaded items = %+v", items)
	}

	if err := reloaded.Delete(item.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := reloaded.List(); len(got) != 0 {
		t.Fatalf("List() after delete = %+v", got)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var persisted []Item
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("persisted JSON invalid: %v", err)
	}
	if len(persisted) != 0 {
		t.Fatalf("persisted items = %+v", persisted)
	}
}

func TestStoreValidationAndClearCompleted(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "shopping-list.json")
	s, err := New(filePath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	s.now = fixedClock()
	s.newID = sequenceID("a", "b")

	if _, err := s.Create(CreateItem{Name: " "}); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("Create(empty) error = %v, want ErrEmptyName", err)
	}

	first, err := s.Create(CreateItem{Name: "Mleko"})
	if err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	second, err := s.Create(CreateItem{Name: "Jablka"})
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}

	done := true
	if _, err := s.Update(first.ID, UpdateItem{Completed: &done}); err != nil {
		t.Fatalf("Update(completed) error = %v", err)
	}
	removed, err := s.ClearCompleted()
	if err != nil {
		t.Fatalf("ClearCompleted() error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("ClearCompleted() removed = %d, want 1", removed)
	}
	items := s.List()
	if len(items) != 1 || items[0].ID != second.ID {
		t.Fatalf("List() after clear = %+v", items)
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
