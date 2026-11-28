package taskstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	ts "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func open(t *testing.T, path string, user string) *Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(db, func(context.Context) (string, error) { return user, nil })
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func task(id, ctx string, state a2a.TaskState) *a2a.Task {
	return &a2a.Task{ID: a2a.TaskID(id), ContextID: ctx, Status: a2a.TaskStatus{State: state,
		Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("waiting"))}}
}

func TestTaskStaysThroughReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	s := open(t, path, "u")
	ctx := t.Context()
	if v, err := s.Create(ctx, task("t1", "c1", a2a.TaskStateSubmitted)); err != nil || v != 1 {
		t.Fatalf("create: %v %v", v, err)
	}
	if _, err := s.Create(ctx, task("t1", "c1", a2a.TaskStateSubmitted)); !errors.Is(err, ts.ErrTaskAlreadyExists) {
		t.Fatalf("second create: %v", err)
	}
	v, err := s.Update(ctx, &ts.UpdateRequest{Task: task("t1", "c1", a2a.TaskStateInputRequired), PrevVersion: 1})
	if err != nil || v != 2 {
		t.Fatalf("update: %v %v", v, err)
	}
	if _, err := s.Update(ctx, &ts.UpdateRequest{Task: task("t1", "c1", a2a.TaskStateWorking), PrevVersion: 1}); !errors.Is(err, ts.ErrConcurrentModification) {
		t.Fatalf("stale update: %v", err)
	}

	got, err := open(t, path, "u").Get(ctx, "t1")
	if err != nil || got.Task.Status.State != a2a.TaskStateInputRequired || got.Version != 2 || got.Task.Status.Message.Parts[0].Text() != "waiting" {
		t.Fatalf("after reopen: %+v %v", got, err)
	}
	if _, err := open(t, path, "someone else").Get(ctx, "t1"); !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("other user: %v", err)
	}
}

func TestListPagesNewestFirst(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "t.db"), "u")
	ctx := t.Context()
	for _, id := range []string{"a", "b", "c"} {
		if _, err := s.Create(ctx, task(id, "c1", a2a.TaskStateCompleted)); err != nil {
			t.Fatal(err)
		}
	}
	s.Create(ctx, task("x", "c2", a2a.TaskStateCompleted))
	p1, err := s.List(ctx, &a2a.ListTasksRequest{ContextID: "c1", PageSize: 2})
	if err != nil || len(p1.Tasks) != 2 || p1.TotalSize != 3 || p1.NextPageToken == "" {
		t.Fatalf("page 1: %+v %v", p1, err)
	}
	p2, err := s.List(ctx, &a2a.ListTasksRequest{ContextID: "c1", PageSize: 2, PageToken: p1.NextPageToken})
	if err != nil || len(p2.Tasks) != 1 || p2.NextPageToken != "" {
		t.Fatalf("page 2: %+v %v", p2, err)
	}
	if _, err := open(t, filepath.Join(t.TempDir(), "x.db"), "").List(ctx, &a2a.ListTasksRequest{}); !errors.Is(err, a2a.ErrUnauthenticated) {
		t.Fatalf("anonymous list: %v", err)
	}
}
