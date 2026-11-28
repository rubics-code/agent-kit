package approval

import (
	"errors"

	"google.golang.org/adk/v2/agent"
)

// Decision is the result of Gate.
type Decision int

const (
	// Pending means that the gate asked a person, and the tool must stop now.
	Pending Decision = iota
	// Approved means that a person approved these exact arguments.
	Approved
	// Rejected means that a person said no.
	Rejected
)

// Gate asks a person to approve a tool call, and binds the approval to the
// arguments. Code gives the summary. The model never writes it.
//
// On the first call, Gate requests a confirmation and returns Pending. When
// ADK runs the tool again with the answer, Gate returns Approved or Rejected.
// An approval for other arguments is an error.
func Gate(ctx agent.Context, tool, summary string, args any) (Decision, error) {
	c := ctx.ToolConfirmation()
	if c == nil {
		ap, err := NewApproval(tool, summary, args)
		if err != nil {
			return Pending, err
		}
		return Pending, ctx.RequestConfirmation(ap.Summary, ap)
	}
	if !c.Confirmed {
		return Rejected, nil
	}
	if err := CheckBinding(tool, args, c.Payload); err != nil {
		return Rejected, errors.New("not done: " + err.Error())
	}
	return Approved, nil
}
