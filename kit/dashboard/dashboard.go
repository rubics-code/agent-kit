// Package dashboard keeps an agent's dashboard cards in SQL, and gives the
// agent a tool that puts or updates a card. The tool writes only to the
// agent's own dashboard, so it needs no approval.
package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"gorm.io/gorm"

	"github.com/rubics-code/agent-kit/kit/a2ui"
)

type row struct {
	CardID    string `gorm:"primaryKey"`
	Title     string
	ThreadID  string
	A2UI      []byte
	Source    string    // "agent" or "person"
	CreatedAt time.Time `gorm:"autoCreateTime:false"`
	UpdatedAt time.Time `gorm:"index;autoUpdateTime:false"`
}

func (row) TableName() string { return "dashboard_cards" }

// Card is one dashboard card as the console reads it.
type Card struct {
	CardID    string    `json:"cardId"`
	Title     string    `json:"title"`
	ThreadID  string    `json:"threadId,omitempty"`
	A2UI      []any     `json:"a2ui"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Store is the dashboard of one agent.
type Store struct {
	db    *gorm.DB
	agent string
	now   func() time.Time
}

func New(db *gorm.DB, agentName string) (*Store, error) {
	if err := db.AutoMigrate(&row{}); err != nil {
		return nil, err
	}
	return &Store{db: db, agent: agentName, now: func() time.Time { return time.Now().UTC() }}, nil
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Put adds a card, or updates the card with the same id.
func (s *Store) Put(ctx context.Context, c Card) (Card, error) {
	if !idRe.MatchString(c.CardID) {
		return Card{}, fmt.Errorf("the card id %q must be 1 to 64 lower-case letters, digits and dashes", c.CardID)
	}
	if strings.TrimSpace(c.Title) == "" {
		return Card{}, errors.New("the card needs a title")
	}
	b, err := json.Marshal(c.A2UI)
	if err != nil {
		return Card{}, err
	}
	if c.Source == "" {
		c.Source = "agent"
	}
	now := s.now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cur row
		err := tx.Where("card_id = ?", c.CardID).First(&cur).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			cur = row{CardID: c.CardID, CreatedAt: now}
		case err != nil:
			return err
		}
		cur.Title, cur.ThreadID, cur.A2UI, cur.Source, cur.UpdatedAt = c.Title, c.ThreadID, b, c.Source, now
		return tx.Save(&cur).Error
	})
	if err != nil {
		return Card{}, err
	}
	return s.get(ctx, c.CardID)
}

func (s *Store) get(ctx context.Context, id string) (Card, error) {
	var r row
	if err := s.db.WithContext(ctx).Where("card_id = ?", id).First(&r).Error; err != nil {
		return Card{}, err
	}
	return toCard(r)
}

func toCard(r row) (Card, error) {
	c := Card{CardID: r.CardID, Title: r.Title, ThreadID: r.ThreadID, Source: r.Source, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	return c, json.Unmarshal(r.A2UI, &c.A2UI)
}

func (s *Store) List(ctx context.Context) ([]Card, error) {
	var rows []row
	if err := s.db.WithContext(ctx).Order("updated_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Card, 0, len(rows))
	for _, r := range rows {
		c, err := toCard(r)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Where("card_id = ?", id).Delete(&row{}).Error
}

type putArgs struct {
	CardID   string        `json:"cardId" jsonschema:"A short id for the card, in lower case with dashes, for example open-notes. The same id updates the card."`
	Title    string        `json:"title" jsonschema:"The title on the card."`
	Columns  []string      `json:"columns,omitempty" jsonschema:"For a table: the column names."`
	Rows     [][]string    `json:"rows,omitempty" jsonschema:"For a table: the rows, one string for each column."`
	Items    []string      `json:"items,omitempty" jsonschema:"For a list: one line for each item."`
	Statuses []a2ui.Status `json:"statuses,omitempty" jsonschema:"For a status list: a label, a state and a tone (ok, warn, idle or bad) for each row."`
	Text     string        `json:"text,omitempty" jsonschema:"For a text card: the text."`
}

type putResult struct {
	Status    string `json:"status"`
	CardID    string `json:"cardId"`
	UpdatedAt string `json:"updatedAt"`
}

// body builds the card's A2UI from the arguments. The model gives data, and
// code makes the UI.
func body(a putArgs) ([]any, error) {
	sid := "dash-" + a.CardID
	switch {
	case len(a.Columns) > 0:
		for i, r := range a.Rows {
			if len(r) != len(a.Columns) {
				return nil, fmt.Errorf("row %d has %d values, but there are %d columns", i+1, len(r), len(a.Columns))
			}
		}
		return a2ui.TableBody(sid, a.Columns, a.Rows), nil
	case len(a.Statuses) > 0:
		return a2ui.StatusBody(sid, a.Statuses), nil
	case len(a.Items) > 0:
		st := make([]a2ui.Status, len(a.Items))
		for i, it := range a.Items {
			st[i] = a2ui.Status{Label: it, Tone: "idle"}
		}
		return a2ui.StatusBody(sid, st), nil
	case a.Text != "":
		return a2ui.TextBody(sid, a.Text), nil
	}
	return nil, errors.New("give columns and rows, statuses, items or text")
}

// Tool gives the put_dashboard_card tool.
func (s *Store) Tool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "put_dashboard_card",
		Description: "Put a card on the workspace dashboard, or update it. Use it when a person asks to keep something on the dashboard. " +
			"Give a table (columns and rows), a status list, a list of items, or a text. The same cardId updates the card.",
	}, func(ctx agent.Context, a putArgs) (putResult, error) {
		b, err := body(a)
		if err != nil {
			return putResult{}, err
		}
		c, err := s.Put(ctx, Card{CardID: a.CardID, Title: a.Title, ThreadID: ctx.SessionID(), A2UI: b, Source: "agent"})
		if err != nil {
			return putResult{}, err
		}
		return putResult{Status: "on the dashboard", CardID: c.CardID, UpdatedAt: c.UpdatedAt.Format(time.RFC3339)}, nil
	})
}

// Handler serves GET /kit/dashboard, POST /kit/dashboard (a person adds a
// card from a thread) and DELETE /kit/dashboard/{id}.
func (s *Store) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /kit/dashboard", func(w http.ResponseWriter, r *http.Request) {
		cards, err := s.List(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"agent": s.agent, "cards": cards})
	})
	mux.HandleFunc("POST /kit/dashboard", func(w http.ResponseWriter, r *http.Request) {
		var c Card
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&c); err != nil {
			http.Error(w, "bad card: "+err.Error(), http.StatusBadRequest)
			return
		}
		c.Source = "person"
		out, err := s.Put(r.Context(), c)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("DELETE /kit/dashboard/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Delete(r.Context(), r.PathValue("id")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
