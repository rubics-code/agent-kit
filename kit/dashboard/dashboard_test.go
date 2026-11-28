package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "d.db")), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(db, "devagent")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPutKeepsTheFirstTimeAndUpdates(t *testing.T) {
	s := newStore(t)
	clock := time.Date(2026, 11, 28, 1, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	ctx := t.Context()
	if _, err := s.Put(ctx, Card{CardID: "groceries", Title: "Groceries", A2UI: []any{}}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Hour)
	c, err := s.Put(ctx, Card{CardID: "groceries", Title: "Groceries, new", A2UI: []any{}})
	if err != nil || c.Title != "Groceries, new" || !c.UpdatedAt.Equal(clock) || c.CreatedAt.Equal(clock) {
		t.Fatalf("update: %+v %v", c, err)
	}
	if _, err := s.Put(ctx, Card{CardID: "Bad Id", Title: "x"}); err == nil {
		t.Fatal("a bad id passed")
	}
	if _, err := s.Put(ctx, Card{CardID: "ok", Title: " "}); err == nil {
		t.Fatal("an empty title passed")
	}
}

func TestBodyComesFromCode(t *testing.T) {
	b, err := body(putArgs{CardID: "shop", Title: "Shop", Items: []string{"milk", "eggs"}})
	if err != nil {
		t.Fatal(err)
	}
	j, _ := json.Marshal(b)
	if !strings.Contains(string(j), `"component":"StatusList"`) || !strings.Contains(string(j), `"label":"milk"`) {
		t.Fatalf("items: %s", j)
	}
	if _, err := body(putArgs{CardID: "t", Columns: []string{"a", "b"}, Rows: [][]string{{"1"}}}); err == nil {
		t.Fatal("a short row passed")
	}
	if _, err := body(putArgs{CardID: "t"}); err == nil {
		t.Fatal("an empty card passed")
	}
}

func TestHandler(t *testing.T) {
	s := newStore(t)
	h := s.Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/kit/dashboard", strings.NewReader(`{"cardId":"notes","title":"Notes","threadId":"c1","a2ui":[{"version":"1.0"}],"source":"agent"}`)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"source":"person"`) {
		t.Fatalf("post: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/kit/dashboard", nil))
	var got struct {
		Agent string `json:"agent"`
		Cards []Card `json:"cards"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Agent != "devagent" || len(got.Cards) != 1 || got.Cards[0].ThreadID != "c1" {
		t.Fatalf("get: %s %v", w.Body, err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/kit/dashboard/notes", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	if cards, _ := s.List(t.Context()); len(cards) != 0 {
		t.Fatalf("after delete: %+v", cards)
	}
}
