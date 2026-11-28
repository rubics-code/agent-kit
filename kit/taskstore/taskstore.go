// Package taskstore keeps A2A tasks in SQL through GORM, so that a task that
// waits for input stays through a restart of the process.
package taskstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	ts "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"gorm.io/gorm"
)

type row struct {
	ID        string `gorm:"primaryKey"`
	ContextID string `gorm:"index"`
	User      string `gorm:"index"`
	State     string
	Version   int64
	Task      []byte
	UpdatedAt time.Time `gorm:"index"`
}

func (row) TableName() string { return "a2a_tasks" }

// Store implements the a2a-go task store on a GORM database.
type Store struct {
	db   *gorm.DB
	auth ts.Authenticator
	now  func() time.Time
}

var _ ts.Store = (*Store)(nil)

// New migrates the table and gives a Store. A nil auth means one anonymous user.
func New(db *gorm.DB, auth ts.Authenticator) (*Store, error) {
	if err := db.AutoMigrate(&row{}); err != nil {
		return nil, err
	}
	if auth == nil {
		auth = func(context.Context) (string, error) { return "", nil }
	}
	return &Store{db: db, auth: auth, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Store) encode(t *a2a.Task) ([]byte, error) {
	if t == nil || t.ID == "" {
		return nil, fmt.Errorf("task without an id: %w", a2a.ErrInvalidRequest)
	}
	return json.Marshal(t)
}

func (s *Store) Create(ctx context.Context, task *a2a.Task) (ts.TaskVersion, error) {
	b, err := s.encode(task)
	if err != nil {
		return ts.TaskVersionMissing, err
	}
	user, err := s.auth(ctx)
	if err != nil {
		return ts.TaskVersionMissing, fmt.Errorf("taskstore auth failed: %w", err)
	}
	r := row{ID: string(task.ID), ContextID: task.ContextID, User: user, State: string(task.Status.State), Version: 1, Task: b, UpdatedAt: s.now()}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&row{}).Where("id = ?", r.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ts.ErrTaskAlreadyExists
		}
		return tx.Create(&r).Error
	})
	if err != nil {
		return ts.TaskVersionMissing, err
	}
	return 1, nil
}

func (s *Store) Update(ctx context.Context, req *ts.UpdateRequest) (ts.TaskVersion, error) {
	b, err := s.encode(req.Task)
	if err != nil {
		return ts.TaskVersionMissing, err
	}
	user, err := s.auth(ctx)
	if err != nil {
		return ts.TaskVersionMissing, fmt.Errorf("taskstore auth failed: %w", err)
	}
	var version ts.TaskVersion
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cur row
		if err := tx.Where("id = ? AND user = ?", string(req.Task.ID), user).First(&cur).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return a2a.ErrTaskNotFound
			}
			return err
		}
		if req.PrevVersion != ts.TaskVersionMissing && ts.TaskVersion(cur.Version) != req.PrevVersion {
			return ts.ErrConcurrentModification
		}
		res := tx.Model(&row{}).Where("id = ? AND version = ?", cur.ID, cur.Version).Updates(map[string]any{
			"context_id": req.Task.ContextID, "state": string(req.Task.Status.State),
			"version": cur.Version + 1, "task": b, "updated_at": s.now(),
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ts.ErrConcurrentModification
		}
		version = ts.TaskVersion(cur.Version + 1)
		return nil
	})
	return version, err
}

func (s *Store) Get(ctx context.Context, id a2a.TaskID) (*ts.StoredTask, error) {
	user, err := s.auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("taskstore auth failed: %w", err)
	}
	var r row
	if err := s.db.WithContext(ctx).Where("id = ? AND user = ?", string(id), user).First(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, a2a.ErrTaskNotFound
		}
		return nil, err
	}
	var t a2a.Task
	if err := json.Unmarshal(r.Task, &t); err != nil {
		return nil, err
	}
	return &ts.StoredTask{Task: &t, Version: ts.TaskVersion(r.Version), User: r.User}, nil
}

// List gives the caller's tasks, newest first. Like the in-memory store, it
// needs an authenticated user.
func (s *Store) List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	user, err := s.auth(ctx)
	if user == "" || err != nil {
		return nil, a2a.ErrUnauthenticated
	}
	size := req.PageSize
	if size == 0 {
		size = 50
	} else if size < 1 || size > 100 {
		return nil, fmt.Errorf("page size must be between 1 and 100, got %d: %w", size, a2a.ErrInvalidRequest)
	}
	q := s.db.WithContext(ctx).Model(&row{}).Where("user = ?", user)
	if req.ContextID != "" {
		q = q.Where("context_id = ?", req.ContextID)
	}
	if req.Status != a2a.TaskStateUnspecified {
		q = q.Where("state = ?", string(req.Status))
	}
	if req.StatusTimestampAfter != nil {
		q = q.Where("updated_at >= ?", req.StatusTimestampAfter.UTC())
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	if req.PageToken != "" {
		at, id, err := decodeToken(req.PageToken)
		if err != nil {
			return nil, err
		}
		q = q.Where("updated_at < ? OR (updated_at = ? AND id < ?)", at, at, id)
	}
	var rows []row
	if err := q.Order("updated_at DESC, id DESC").Limit(size + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	next := ""
	if len(rows) > size {
		rows = rows[:size]
		last := rows[size-1]
		next = encodeToken(last.UpdatedAt, last.ID)
	}
	hist := 100
	if req.HistoryLength != nil {
		hist = *req.HistoryLength
	}
	out := &a2a.ListTasksResponse{Tasks: []*a2a.Task{}, TotalSize: int(total), PageSize: size, NextPageToken: next}
	for _, r := range rows {
		var t a2a.Task
		if err := json.Unmarshal(r.Task, &t); err != nil {
			return nil, err
		}
		if hist <= 0 {
			t.History = []*a2a.Message{}
		} else if len(t.History) > hist {
			t.History = t.History[len(t.History)-hist:]
		}
		if !req.IncludeArtifacts {
			t.Artifacts = nil
		}
		out.Tasks = append(out.Tasks, &t)
	}
	return out, nil
}

func encodeToken(at time.Time, id string) string {
	return base64.URLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "_" + id))
}

func decodeToken(tok string) (time.Time, string, error) {
	b, err := base64.URLEncoding.DecodeString(tok)
	if err != nil {
		return time.Time{}, "", a2a.ErrParseError
	}
	at, id, ok := strings.Cut(string(b), "_")
	if !ok {
		return time.Time{}, "", a2a.ErrParseError
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, "", a2a.ErrParseError
	}
	return t.UTC(), id, nil
}
