package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"lista-zakupow/internal/store"
)

func TestAPIItemsLifecycle(t *testing.T) {
	handler := newTestHandler(t)

	item := requestJSON[store.Item](t, handler, http.MethodPost, "/api/items", `{"name":"Kawa","note":"250 g"}`, http.StatusCreated)
	if item.ID == "" || item.Name != "Kawa" || item.Note != "250 g" {
		t.Fatalf("created item = %+v", item)
	}

	items := requestJSON[[]store.Item](t, handler, http.MethodGet, "/api/items", "", http.StatusOK)
	if len(items) != 1 {
		t.Fatalf("GET items len = %d, want 1", len(items))
	}

	updated := requestJSON[store.Item](t, handler, http.MethodPatch, "/api/items/"+item.ID, `{"completed":true}`, http.StatusOK)
	if !updated.Completed {
		t.Fatalf("updated item = %+v", updated)
	}

	clear := requestJSON[map[string]int](t, handler, http.MethodPost, "/api/items/clear-completed", "", http.StatusOK)
	if clear["removed"] != 1 {
		t.Fatalf("clear response = %+v", clear)
	}

	items = requestJSON[[]store.Item](t, handler, http.MethodGet, "/api/items", "", http.StatusOK)
	if len(items) != 0 {
		t.Fatalf("GET items after clear = %+v", items)
	}
}

func TestAPIErrors(t *testing.T) {
	handler := newTestHandler(t)

	requestJSON[map[string]string](t, handler, http.MethodPost, "/api/items", `{"name":" "}`, http.StatusBadRequest)
	requestJSON[map[string]string](t, handler, http.MethodPatch, "/api/items/missing", `{"completed":true}`, http.StatusNotFound)
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()

	s, err := store.New(filepath.Join(t.TempDir(), "shopping-list.json"))
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	api := New(s, http.NotFoundHandler(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return api.Routes()
}

func requestJSON[T any](t *testing.T, handler http.Handler, method, path, body string, wantStatus int) T {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d, body = %s", method, path, rec.Code, wantStatus, rec.Body.String())
	}

	var out T
	if rec.Body.Len() == 0 {
		return out
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	return out
}
