// Package policy is an agent's identity and limits, as data (ADR-0003). The
// Kit writes the approval and rejection copy from it. The model never writes
// that copy.
package policy

import "fmt"

// Infisical says which Infisical paths an agent may read and write, and how a
// path is labelled in copy (which environment it belongs to).
type Infisical struct {
	// Readable and Writable return an error for a path that the agent may not use.
	Readable func(path string) error
	Writable func(path string) error
	EnvLabel func(path string) string
}

// Policy is passed as data into the Kit's machinery. Config (URLs, ports,
// model ids) stays with the agent.
type Policy struct {
	Agent          string
	Operator       string
	MaxKeyLimitUSD float64
	Infisical      Infisical
}

// Rejected is the copy for a write that the operator did not approve.
func (p Policy) Rejected(reason string) string {
	op := p.Operator
	if op == "" {
		op = "the operator"
	}
	if reason == "" {
		reason = "rejected"
	}
	return fmt.Sprintf("Not done: %s did not approve (%s). Do not try again unless %s asks.", op, reason, op)
}
