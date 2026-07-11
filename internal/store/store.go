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

type DataFile struct {
	Lists []ShoppingList `json:"lists"`
}

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
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
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

type Store struct {
	mu       sync.Mutex
	filePath string
	lists    []ShoppingList
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

func (s *Store) Lists() []ListSummary {
	s.mu.Lock()
	defer s.mu.Unlock()

	return summarizeLists(s.lists)
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
	list := ShoppingList{
		ID:        id,
		Name:      name,
		Items:     []Item{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.lists = append(s.lists, list)
	if err := s.saveLocked(); err != nil {
		s.lists = s.lists[:len(s.lists)-1]
		return ListSummary{}, err
	}

	return summarizeList(list), nil
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

	index := s.listIndexLocked(id)
	if index == -1 {
		return ListSummary{}, ErrListNotFound
	}

	previous := s.lists[index]
	s.lists[index].Name = name
	s.lists[index].UpdatedAt = s.now()
	if err := s.saveLocked(); err != nil {
		s.lists[index] = previous
		return ListSummary{}, err
	}

	return summarizeList(s.lists[index]), nil
}

func (s *Store) DeleteList(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrBadListID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.listIndexLocked(id)
	if index == -1 {
		return ErrListNotFound
	}

	previous := cloneLists(s.lists)
	s.lists = append(s.lists[:index], s.lists[index+1:]...)
	if len(s.lists) == 0 {
		list, err := s.defaultListLocked()
		if err != nil {
			s.lists = previous
			return err
		}
		s.lists = []ShoppingList{list}
	}
	if err := s.saveLocked(); err != nil {
		s.lists = previous
		return err
	}

	return nil
}

func (s *Store) ListItems(listID string) ([]Item, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return nil, ErrBadListID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.listIndexLocked(listID)
	if index == -1 {
		return nil, ErrListNotFound
	}
	return cloneItems(s.lists[index].Items), nil
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

	index := s.listIndexLocked(listID)
	if index == -1 {
		return Item{}, ErrListNotFound
	}

	previous := cloneLists(s.lists)
	s.lists[index].Items = append(s.lists[index].Items, item)
	s.lists[index].UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		s.lists = previous
		return Item{}, err
	}

	return item, nil
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

	listIndex := s.listIndexLocked(listID)
	if listIndex == -1 {
		return Item{}, ErrListNotFound
	}
	itemIndex := itemIndex(s.lists[listIndex].Items, itemID)
	if itemIndex == -1 {
		return Item{}, ErrNotFound
	}

	previous := cloneLists(s.lists)
	next := s.lists[listIndex].Items[itemIndex]
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
	now := s.now()
	next.UpdatedAt = now

	s.lists[listIndex].Items[itemIndex] = next
	s.lists[listIndex].UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		s.lists = previous
		return Item{}, err
	}

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

	listIndex := s.listIndexLocked(listID)
	if listIndex == -1 {
		return ErrListNotFound
	}
	itemIndex := itemIndex(s.lists[listIndex].Items, itemID)
	if itemIndex == -1 {
		return ErrNotFound
	}

	previous := cloneLists(s.lists)
	s.lists[listIndex].Items = append(s.lists[listIndex].Items[:itemIndex], s.lists[listIndex].Items[itemIndex+1:]...)
	s.lists[listIndex].UpdatedAt = s.now()
	if err := s.saveLocked(); err != nil {
		s.lists = previous
		return err
	}

	return nil
}

func (s *Store) ClearCompletedItems(listID string) (int, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return 0, ErrBadListID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	listIndex := s.listIndexLocked(listID)
	if listIndex == -1 {
		return 0, ErrListNotFound
	}

	previous := cloneLists(s.lists)
	items := s.lists[listIndex].Items
	next := items[:0]
	removed := 0
	for _, item := range items {
		if item.Completed {
			removed++
			continue
		}
		next = append(next, item)
	}
	if removed == 0 {
		return 0, nil
	}

	s.lists[listIndex].Items = next
	s.lists[listIndex].UpdatedAt = s.now()
	if err := s.saveLocked(); err != nil {
		s.lists = previous
		return 0, err
	}

	return removed, nil
}

func (s *Store) List() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()

	return cloneItems(s.lists[0].Items)
}

func (s *Store) Create(input CreateItem) (Item, error) {
	return s.CreateItem(s.DefaultListID(), input)
}

func (s *Store) Update(id string, patch UpdateItem) (Item, error) {
	return s.UpdateItem(s.DefaultListID(), id, patch)
}

func (s *Store) Delete(id string) error {
	return s.DeleteItem(s.DefaultListID(), id)
}

func (s *Store) ClearCompleted() (int, error) {
	return s.ClearCompletedItems(s.DefaultListID())
}

func (s *Store) DefaultListID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.lists[0].ID
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return s.ensureDefaultListLocked()
	}
	if err != nil {
		return fmt.Errorf("read data file: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return s.ensureDefaultListLocked()
	}

	var document DataFile
	if err := json.Unmarshal(data, &document); err == nil && document.Lists != nil {
		s.lists = normalizeLists(document.Lists)
		return s.ensureDefaultListLocked()
	}

	var legacyItems []Item
	if err := json.Unmarshal(data, &legacyItems); err != nil {
		return fmt.Errorf("decode data file: %w", err)
	}
	list, err := s.defaultListLocked()
	if err != nil {
		return err
	}
	list.Items = normalizeItems(legacyItems)
	s.lists = []ShoppingList{list}
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if dir == "" {
		return ErrBadDataDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	data, err := json.MarshalIndent(DataFile{Lists: s.lists}, "", "  ")
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

func (s *Store) ensureDefaultListLocked() error {
	if len(s.lists) > 0 {
		return nil
	}
	list, err := s.defaultListLocked()
	if err != nil {
		return err
	}
	s.lists = []ShoppingList{list}
	return nil
}

func (s *Store) defaultListLocked() (ShoppingList, error) {
	id, err := s.newID()
	if err != nil {
		return ShoppingList{}, fmt.Errorf("create default list id: %w", err)
	}
	now := s.now()
	return ShoppingList{
		ID:        id,
		Name:      DefaultListName,
		Items:     []Item{},
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *Store) listIndexLocked(id string) int {
	for i := range s.lists {
		if s.lists[i].ID == id {
			return i
		}
	}
	return -1
}

func summarizeLists(lists []ShoppingList) []ListSummary {
	out := make([]ListSummary, len(lists))
	for i := range lists {
		out[i] = summarizeList(lists[i])
	}
	return out
}

func summarizeList(list ShoppingList) ListSummary {
	done := 0
	for _, item := range list.Items {
		if item.Completed {
			done++
		}
	}
	total := len(list.Items)
	return ListSummary{
		ID:         list.ID,
		Name:       list.Name,
		TotalCount: total,
		OpenCount:  total - done,
		DoneCount:  done,
		CreatedAt:  list.CreatedAt,
		UpdatedAt:  list.UpdatedAt,
	}
}

func normalizeLists(lists []ShoppingList) []ShoppingList {
	out := make([]ShoppingList, 0, len(lists))
	for _, list := range lists {
		list.ID = strings.TrimSpace(list.ID)
		list.Name = strings.TrimSpace(list.Name)
		if list.ID == "" || list.Name == "" {
			continue
		}
		list.Items = normalizeItems(list.Items)
		out = append(out, list)
	}
	return out
}

func normalizeItems(items []Item) []Item {
	out := make([]Item, len(items))
	copy(out, items)
	if out == nil {
		return []Item{}
	}
	return out
}

func cloneLists(lists []ShoppingList) []ShoppingList {
	out := make([]ShoppingList, len(lists))
	for i := range lists {
		out[i] = lists[i]
		out[i].Items = cloneItems(lists[i].Items)
	}
	return out
}

func cloneItems(items []Item) []Item {
	out := make([]Item, len(items))
	copy(out, items)
	return out
}

func itemIndex(items []Item, id string) int {
	for i := range items {
		if items[i].ID == id {
			return i
		}
	}
	return -1
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
