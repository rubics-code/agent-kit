package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Approval is the payload that the Kit puts on a confirmation. Code makes it
// from the arguments. The model never writes it.
type Approval struct {
	Tool     string         `json:"tool"`
	Summary  string         `json:"summary"`
	ArgsHash string         `json:"argsHash"`
	Args     map[string]any `json:"args"`
}

// canonicalArgs gives the arguments as a map with sorted keys when marshalled.
func canonicalArgs(args any) (map[string]any, []byte, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, err
	}
	canon, err := json.Marshal(m)
	if err != nil {
		return nil, nil, err
	}
	return m, canon, nil
}

func ArgsHash(tool string, args any) (string, error) {
	_, canon, err := canonicalArgs(args)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte(tool+"\n"), canon...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func NewApproval(tool, summary string, args any) (Approval, error) {
	m, _, err := canonicalArgs(args)
	if err != nil {
		return Approval{}, err
	}
	h, err := ArgsHash(tool, args)
	if err != nil {
		return Approval{}, err
	}
	return Approval{Tool: tool, Summary: summary, ArgsHash: h, Args: m}, nil
}

// CheckBinding returns nil only when the approver sent back the hash of these
// exact arguments.
func CheckBinding(tool string, args any, payload any) error {
	want, err := ArgsHash(tool, args)
	if err != nil {
		return err
	}
	got := ""
	if m, ok := payload.(map[string]any); ok {
		got, _ = m["argsHash"].(string)
	}
	if got == "" {
		return fmt.Errorf("the approval has no argsHash")
	}
	if got != want {
		return fmt.Errorf("the approval is for other arguments (approved %s, call has %s)", got, want)
	}
	return nil
}
