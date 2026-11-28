package main

import (
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"gorm.io/gorm"

	"github.com/rubics-code/agent-kit/kit/approval"
)

type Note struct {
	Title string `gorm:"primaryKey" json:"title"`
	Body  string `json:"body"`
}

type listArgs struct {
	Prefix string `json:"prefix,omitempty" jsonschema:"Only notes whose title starts with this text. Empty gives all notes."`
}

type listResult struct {
	Notes []Note `json:"notes"`
}

type writeArgs struct {
	Title string `json:"title" jsonschema:"The title of the note. A note with the same title is replaced."`
	Body  string `json:"body" jsonschema:"The text of the note."`
}

type writeResult struct {
	Status string `json:"status"`
	Title  string `json:"title,omitempty"`
}

func writeSummary(a writeArgs) string {
	body := a.Body
	if r := []rune(body); len(r) > 80 {
		body = string(r[:80]) + "…"
	}
	return fmt.Sprintf("Write the note %q (%d characters): %q", a.Title, len([]rune(a.Body)), body)
}

func noteTools(db *gorm.DB) ([]tool.Tool, error) {
	if err := db.AutoMigrate(&Note{}); err != nil {
		return nil, err
	}
	list, err := functiontool.New(functiontool.Config{
		Name:        "list_notes",
		Description: "List the notes in the notebook, with their text.",
	}, func(_ agent.Context, a listArgs) (listResult, error) {
		notes := []Note{}
		q := db.Order("title")
		if a.Prefix != "" {
			q = q.Where("title LIKE ? ESCAPE '\\'", strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(a.Prefix)+"%")
		}
		err := q.Find(&notes).Error
		return listResult{Notes: notes}, err
	})
	if err != nil {
		return nil, err
	}
	write, err := functiontool.New(functiontool.Config{
		Name:        "write_note",
		Description: "Write a note to the notebook. A person must approve each write.",
	}, func(ctx agent.Context, a writeArgs) (writeResult, error) {
		d, err := approval.Gate(ctx, "write_note", writeSummary(a), a)
		switch {
		case err != nil:
			return writeResult{}, err
		case d == approval.Pending:
			return writeResult{Status: "waiting for approval"}, nil
		case d == approval.Rejected:
			return writeResult{Status: "rejected"}, nil
		}
		if err := db.Save(&Note{Title: a.Title, Body: a.Body}).Error; err != nil {
			return writeResult{}, err
		}
		return writeResult{Status: "written", Title: a.Title}, nil
	})
	if err != nil {
		return nil, err
	}
	return []tool.Tool{list, write}, nil
}
