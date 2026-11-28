// Package briefing makes the agent's system prompt: the prompt from its home,
// each group's guidance, and the capability note. The policy fills the
// placeholders. The model never writes any of it.
package briefing

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/policy"
)

// Home is an agent's home folder:
//
//	prompts/system.md   the system prompt (required)
//	policy.json         the policy (see policy.Data)
//	memory/*.md         memory files, in name order
type Home struct {
	Dir    string
	System string
	Policy policy.Policy
	Memory []File
}

// File is one memory file.
type File struct {
	Name, Text string
}

// LoadHome reads an agent's home.
func LoadHome(dir string) (Home, error) {
	h := Home{Dir: dir}
	sys, err := os.ReadFile(filepath.Join(dir, "prompts", "system.md"))
	if err != nil {
		return h, fmt.Errorf("the agent's home needs prompts/system.md: %w", err)
	}
	h.System = strings.TrimSpace(string(sys))
	if h.Policy, err = policy.Load(filepath.Join(dir, "policy.json")); err != nil {
		return h, err
	}
	h.Memory, err = ReadMemory(filepath.Join(dir, "memory"))
	return h, err
}

// ReadMemory reads the .md files in a folder, in name order. README.md tells
// people about the folder, so it is not memory. A folder that is not there
// gives no files.
func ReadMemory(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.EqualFold(e.Name(), "README.md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if t := strings.TrimSpace(string(b)); t != "" {
			out = append(out, File{Name: e.Name(), Text: t})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Assemble gives the fixed part of the briefing: the system prompt, the
// guidance of each group that is on, and the capability note, with the
// policy's values in the placeholders.
func Assemble(system string, a group.Assembly, p policy.Policy) string {
	parts := []string{p.Fill(system)}
	for _, g := range a.Guidance {
		parts = append(parts, p.Fill(g))
	}
	parts = append(parts, a.Note)
	return strings.Join(parts, "\n\n")
}
