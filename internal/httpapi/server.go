package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"lista-zakupow/internal/store"
)

type Server struct {
	store  *store.Store
	static http.Handler
	logger *slog.Logger
}

func New(s *store.Store, static http.Handler, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{store: s, static: static, logger: logger}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("/api/lists", s.lists)
	mux.HandleFunc("/api/lists/", s.listByID)
	mux.HandleFunc("/api/items", s.items)
	mux.HandleFunc("/api/items/", s.itemByID)
	mux.Handle("/", s.static)
	return s.recover(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) lists(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.store.Lists())
	case http.MethodPost:
		var input store.CreateList
		if !decodeJSON(w, r, &input) {
			return
		}
		list, err := s.store.CreateList(input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, list)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) listByID(w http.ResponseWriter, r *http.Request) {
	listID, rest, ok := parseListPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if rest == "" {
		switch r.Method {
		case http.MethodPatch:
			var input store.UpdateList
			if !decodeJSON(w, r, &input) {
				return
			}
			list, err := s.store.UpdateList(listID, input)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, list)
		case http.MethodDelete:
			if err := s.store.DeleteList(listID); err != nil {
				writeStoreError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			methodNotAllowed(w, http.MethodPatch, http.MethodDelete)
		}
		return
	}

	if rest == "items" {
		s.listItems(w, r, listID)
		return
	}
	if rest == "items/clear-completed" {
		s.clearListCompleted(w, r, listID)
		return
	}
	if strings.HasPrefix(rest, "items/") {
		itemID := strings.TrimPrefix(rest, "items/")
		if itemID == "" || strings.Contains(itemID, "/") {
			http.NotFound(w, r)
			return
		}
		s.listItemByID(w, r, listID, itemID)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request, listID string) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListItems(listID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var input store.CreateItem
		if !decodeJSON(w, r, &input) {
			return
		}
		item, err := s.store.CreateItem(listID, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) listItemByID(w http.ResponseWriter, r *http.Request, listID, itemID string) {
	switch r.Method {
	case http.MethodPatch:
		var input store.UpdateItem
		if !decodeJSON(w, r, &input) {
			return
		}
		item, err := s.store.UpdateItem(listID, itemID, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		if err := s.store.DeleteItem(listID, itemID); err != nil {
			writeStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodPatch, http.MethodDelete)
	}
}

func (s *Server) clearListCompleted(w http.ResponseWriter, r *http.Request, listID string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	removed, err := s.store.ClearCompletedItems(listID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": removed})
}

func (s *Server) items(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.store.List())
	case http.MethodPost:
		var input store.CreateItem
		if !decodeJSON(w, r, &input) {
			return
		}
		item, err := s.store.Create(input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) itemByID(w http.ResponseWriter, r *http.Request) {
	clean := path.Clean(r.URL.Path)
	if clean == "/api/items/clear-completed" {
		s.clearCompleted(w, r)
		return
	}

	id := strings.TrimPrefix(clean, "/api/items/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var input store.UpdateItem
		if !decodeJSON(w, r, &input) {
			return
		}
		item, err := s.store.Update(id, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		if err := s.store.Delete(id); err != nil {
			writeStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodPatch, http.MethodDelete)
	}
}

func (s *Server) clearCompleted(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	removed, err := s.store.ClearCompleted()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": removed})
}

func parseListPath(rawPath string) (string, string, bool) {
	clean := path.Clean(rawPath)
	if clean == "/api/lists" || !strings.HasPrefix(clean, "/api/lists/") {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(clean, "/api/lists/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false
	}
	return parts[0], strings.Join(parts[1:], "/"), true
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic in request", "error", recovered, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "Wystąpił błąd serwera.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "Niepoprawny JSON.")
		return false
	}
	return true
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Nie znaleziono pozycji.")
	case errors.Is(err, store.ErrListNotFound):
		writeError(w, http.StatusNotFound, "Nie znaleziono listy.")
	case errors.Is(err, store.ErrEmptyName), errors.Is(err, store.ErrBadPatch), errors.Is(err, store.ErrBadItemID), errors.Is(err, store.ErrBadListID):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "Wystąpił błąd serwera.")
	}
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "Metoda niedozwolona.")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
