package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound   = errors.New("item not found")
	ErrEmptyName  = errors.New("item name cannot be empty")
	ErrBadPatch   = errors.New("patch does not contain editable fields")
	ErrBadItemID  = errors.New("item id cannot be empty")
	ErrBadDataDir = errors.New("data file path must include a directory")
)

type Item struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Note      string    `json:"note,omitempty"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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

type Store struct {
	mu       sync.Mutex
	filePath string
	items    []Item
	now      func() time.Time
	newID    func() (string, error)
}

func New(filePath string) (*Store, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, ErrBadDataDir
	}

	s := &Store{
		filePath: filePath,
		now:      func() time.Time { return time.Now().UTC() },
		newID:    randomID,
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) List() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()

	return cloneItems(s.items)
}

func (s *Store) Create(input CreateItem) (Item, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Item{}, ErrEmptyName
	}

	id, err := s.newID()
	if err != nil {
		return Item{}, fmt.Errorf("create id: %w", err)
	}

	now := s.now()
	item := Item{
		ID:        id,
		Name:      name,
		Note:      strings.TrimSpace(input.Note),
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = append(s.items, item)
	if err := s.saveLocked(); err != nil {
		s.items = s.items[:len(s.items)-1]
		return Item{}, err
	}

	return item, nil
}

func (s *Store) Update(id string, patch UpdateItem) (Item, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Item{}, ErrBadItemID
	}
	if patch.Name == nil && patch.Note == nil && patch.Completed == nil {
		return Item{}, ErrBadPatch
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index := -1
	for i := range s.items {
		if s.items[i].ID == id {
			index = i
			break
		}
	}
	if index == -1 {
		return Item{}, ErrNotFound
	}

	previous := s.items[index]
	next := previous
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

	s.items[index] = next
	if err := s.saveLocked(); err != nil {
		s.items[index] = previous
		return Item{}, err
	}

	return next, nil
}

func (s *Store) Delete(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrBadItemID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID == id {
			previous := cloneItems(s.items)
			s.items = append(s.items[:i], s.items[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.items = previous
				return err
			}
			return nil
		}
	}

	return ErrNotFound
}

func (s *Store) ClearCompleted() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	previous := cloneItems(s.items)
	next := s.items[:0]
	removed := 0
	for _, item := range s.items {
		if item.Completed {
			removed++
			continue
		}
		next = append(next, item)
	}
	s.items = next

	if removed == 0 {
		return 0, nil
	}
	if err := s.saveLocked(); err != nil {
		s.items = previous
		return 0, err
	}

	return removed, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		s.items = []Item{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read data file: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		s.items = []Item{}
		return nil
	}

	var items []Item
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("decode data file: %w", err)
	}
	s.items = items
	return nil
}

func (s *Store) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if dir == "" {
		return ErrBadDataDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return fmt.Errorf("encode data file: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".shopping-list-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp data file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp data file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp data file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp data file: %w", err)
	}
	if err := os.Rename(tmpName, s.filePath); err != nil {
		return fmt.Errorf("replace data file: %w", err)
	}

	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	return nil
}

func cloneItems(items []Item) []Item {
	out := make([]Item, len(items))
	copy(out, items)
	return out
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
