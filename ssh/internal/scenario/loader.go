package scenario

import (
	"fmt"
	"os"
	"strings"
)

// Scenario holds the raw content of SCENARIO.md.
type Scenario struct {
	content string
	name    string
}

// Load reads a scenario markdown file from disk.
func Load(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenario file %q: %w", path, err)
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil, fmt.Errorf("scenario file %q is empty", path)
	}
	return &Scenario{
		content: content,
		name:    extractTitle(content),
	}, nil
}

func (s *Scenario) Content() string { return s.content }
func (s *Scenario) Name() string    { return s.name }

func extractTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return "Unnamed Scenario"
}
