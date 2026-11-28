package main

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"gorm.io/gorm"
)

type Note struct {
	Title string `gorm:"primaryKey"`
	Body  string
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

const writeToolName = "write_note"

// writeSummary is the approval summary. Code writes it from the arguments.
func writeSummary(a writeArgs) string {
	body := a.Body
	if len(body) > 80 {
		body = body[:80] + "…"
	}
	return fmt.Sprintf("Write the note %q (%d characters): %q", a.Title, len(a.Body), body)
}

func noteTools(db *gorm.DB) ([]tool.Tool, error) {
	list, err := functiontool.New(functiontool.Config{
		Name:        "list_notes",
		Description: "List the notes in the notebook, with their text.",
	}, func(_ agent.Context, a listArgs) (listResult, error) {
		var notes []Note
		q := db.Order("title")
		if a.Prefix != "" {
			q = q.Where("title LIKE ?", strings.ReplaceAll(a.Prefix, "%", `\%`)+"%")
		}
		if err := q.Find(&notes).Error; err != nil {
			return listResult{}, err
		}
		return listResult{Notes: notes}, nil
	})
	if err != nil {
		return nil, err
	}

	write, err := functiontool.New(functiontool.Config{
		Name:        writeToolName,
		Description: "Write a note to the notebook. A person must approve each write.",
	}, func(ctx agent.Context, a writeArgs) (writeResult, error) {
		c := ctx.ToolConfirmation()
		if c == nil {
			ap, err := NewApproval(writeToolName, writeSummary(a), a)
			if err != nil {
				return writeResult{}, err
			}
			if err := ctx.RequestConfirmation(ap.Summary, ap); err != nil {
				return writeResult{}, err
			}
			return writeResult{Status: "waiting for approval"}, nil
		}
		if !c.Confirmed {
			return writeResult{Status: "rejected by the person"}, nil
		}
		if err := CheckBinding(writeToolName, a, c.Payload); err != nil {
			return writeResult{}, errors.New("not written: " + err.Error())
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
