package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"lista-zakupow/internal/store"
)

func TestAPIListsAndScopedItemsLifecycle(t *testing.T) {
	handler := newTestHandler(t)

	home := requestJSON[store.ListSummary](t, handler, http.MethodPost, "/api/lists", `{"name":"Dom"}`, http.StatusCreated)
	weekend := requestJSON[store.ListSummary](t, handler, http.MethodPost, "/api/lists", `{"name":"Weekend"}`, http.StatusCreated)
	if home.ID == "" || home.Name != "Dom" || weekend.ID == "" {
		t.Fatalf("created lists = %+v %+v", home, weekend)
	}

	homeItem := requestJSON[store.Item](t, handler, http.MethodPost, "/api/lists/"+home.ID+"/items", `{"name":"Kawa","note":"250 g"}`, http.StatusCreated)
	weekendItem := requestJSON[store.Item](t, handler, http.MethodPost, "/api/lists/"+weekend.ID+"/items", `{"name":"Kielbasa"}`, http.StatusCreated)
	if homeItem.ID == "" || weekendItem.ID == "" {
		t.Fatalf("created items = %+v %+v", homeItem, weekendItem)
	}

	homeItems := requestJSON[store.ItemsSnapshot](t, handler, http.MethodGet, "/api/lists/"+home.ID+"/items", "", http.StatusOK)
	weekendItems := requestJSON[store.ItemsSnapshot](t, handler, http.MethodGet, "/api/lists/"+weekend.ID+"/items", "", http.StatusOK)
	if len(homeItems.Items) != 1 || homeItems.Items[0].Name != "Kawa" || homeItems.Version == 0 {
		t.Fatalf("home items = %+v", homeItems)
	}
	if len(weekendItems.Items) != 1 || weekendItems.Items[0].Name != "Kielbasa" || weekendItems.Version == 0 {
		t.Fatalf("weekend items = %+v", weekendItems)
	}

	updatedList := requestJSON[store.ListSummary](t, handler, http.MethodPatch, "/api/lists/"+home.ID, `{"name":"Dom i ogród"}`, http.StatusOK)
	if updatedList.Name != "Dom i ogród" {
		t.Fatalf("updated list = %+v", updatedList)
	}

	updatedItem := requestJSON[store.Item](t, handler, http.MethodPatch, "/api/lists/"+home.ID+"/items/"+homeItem.ID, `{"completed":true}`, http.StatusOK)
	if !updatedItem.Completed {
		t.Fatalf("updated item = %+v", updatedItem)
	}
	clear := requestJSON[map[string]int](t, handler, http.MethodPost, "/api/lists/"+home.ID+"/items/clear-completed", "", http.StatusOK)
	if clear["removed"] != 1 {
		t.Fatalf("clear response = %+v", clear)
	}

	requestNoBody(t, handler, http.MethodDelete, "/api/lists/"+weekend.ID+"/items/"+weekendItem.ID, http.StatusNoContent)
	requestNoBody(t, handler, http.MethodDelete, "/api/lists/"+weekend.ID, http.StatusNoContent)

	lists := requestJSON[[]store.ListSummary](t, handler, http.MethodGet, "/api/lists", "", http.StatusOK)
	for _, list := range lists {
		if list.ID == weekend.ID {
			t.Fatalf("deleted list still present in %+v", lists)
		}
	}
}

func TestListEventsWaitsAndReturnsAfterMutation(t *testing.T) {
	api := newTestAPI(t)
	api.longPollTimeout = time.Second
	handler := api.Routes()

	list := requestJSON[store.ListSummary](t, handler, http.MethodPost, "/api/lists", `{"name":"Dom"}`, http.StatusCreated)
	since := list.Version

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/lists/"+list.ID+"/events?since="+strconv.FormatUint(since, 10), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		done <- rec
	}()

	select {
	case rec := <-done:
		t.Fatalf("long poll returned before mutation: status=%d body=%s", rec.Code, rec.Body.String())
	case <-time.After(50 * time.Millisecond):
	}

	requestJSON[store.Item](t, handler, http.MethodPost, "/api/lists/"+list.ID+"/items", `{"name":"Kawa"}`, http.StatusCreated)

	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("events status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var event map[string]uint64
		if err := json.NewDecoder(rec.Body).Decode(&event); err != nil {
			t.Fatalf("decode events response: %v; body = %s", err, rec.Body.String())
		}
		if event["version"] <= since {
			t.Fatalf("events version = %d, want > %d", event["version"], since)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("long poll did not return after mutation")
	}
}

func TestListEventsOldVersionReturnsImmediately(t *testing.T) {
	handler := newTestHandler(t)

	list := requestJSON[store.ListSummary](t, handler, http.MethodPost, "/api/lists", `{"name":"Dom"}`, http.StatusCreated)
	requestJSON[store.Item](t, handler, http.MethodPost, "/api/lists/"+list.ID+"/items", `{"name":"Kawa"}`, http.StatusCreated)

	start := time.Now()
	event := requestJSON[map[string]uint64](t, handler, http.MethodGet, "/api/lists/"+list.ID+"/events?since="+strconv.FormatUint(list.Version, 10), "", http.StatusOK)
	if time.Since(start) > 100*time.Millisecond {
		t.Fatalf("events with old version took too long")
	}
	if event["version"] <= list.Version {
		t.Fatalf("events version = %d, want > %d", event["version"], list.Version)
	}
}

func TestListEventsTimeoutDoesNotBreakNextPoll(t *testing.T) {
	api := newTestAPI(t)
	api.longPollTimeout = 30 * time.Millisecond
	handler := api.Routes()

	list := requestJSON[store.ListSummary](t, handler, http.MethodPost, "/api/lists", `{"name":"Dom"}`, http.StatusCreated)

	start := time.Now()
	event := requestJSON[map[string]uint64](t, handler, http.MethodGet, "/api/lists/"+list.ID+"/events?since="+strconv.FormatUint(list.Version, 10), "", http.StatusOK)
	if time.Since(start) < 20*time.Millisecond {
		t.Fatalf("events timeout returned too early")
	}
	if event["version"] != list.Version {
		t.Fatalf("timeout events version = %d, want %d", event["version"], list.Version)
	}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/lists/"+list.ID+"/events?since="+strconv.FormatUint(list.Version, 10), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		done <- rec
	}()
	time.Sleep(10 * time.Millisecond)
	requestJSON[store.Item](t, handler, http.MethodPost, "/api/lists/"+list.ID+"/items", `{"name":"Mleko"}`, http.StatusCreated)

	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("events after timeout status = %d, body = %s", rec.Code, rec.Body.String())
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("long poll after timeout did not return after mutation")
	}
}

func TestAPIErrors(t *testing.T) {
	handler := newTestHandler(t)

	requestJSON[map[string]string](t, handler, http.MethodGet, "/api/lists/missing/items", "", http.StatusNotFound)
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return newTestAPI(t).Routes()
}

func newTestAPI(t *testing.T) *Server {
	t.Helper()

	s, err := store.New(filepath.Join(t.TempDir(), "shopping-list.sqlite"))
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return New(s, http.NotFoundHandler(), slog.New(slog.NewTextHandler(io.Discard, nil)))
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

func requestNoBody(t *testing.T, handler http.Handler, method, path string, wantStatus int) {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d, body = %s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
}
