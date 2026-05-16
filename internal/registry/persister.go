package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Persister handles JSON file persistence with atomic writes.
type Persister struct {
	path string
}

// NewPersister creates a new Persister for the given file path.
func NewPersister(path string) *Persister {
	return &Persister{path: path}
}

// Path returns the configured file path.
func (p *Persister) Path() string {
	return p.path
}

// Save writes the agents map to disk atomically with indentation (human-readable).
func (p *Persister) Save(agents map[string]*Agent) error {
	return p.save(agents, true)
}

// SaveCompact writes the agents map to disk atomically using compact JSON
// (no indentation). This reduces allocation size compared to Save and is
// suitable for periodic batched flushes where human readability is not needed.
func (p *Persister) SaveCompact(agents map[string]*Agent) error {
	return p.save(agents, false)
}

// save internal helper that marshals with or without indentation.
func (p *Persister) save(agents map[string]*Agent, indent bool) error {
	// Ensure directory exists
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// Marshal to JSON
	var data []byte
	var err error
	if indent {
		data, err = json.MarshalIndent(agents, "", "  ")
	} else {
		data, err = json.Marshal(agents)
	}
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Write to temp file
	tmpPath := p.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, p.path); err != nil {
		_ = os.Remove(tmpPath) // best-effort cleanup
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

// Load reads agents from disk. Returns an empty map if the file doesn't exist.
func (p *Persister) Load() (map[string]*Agent, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*Agent), nil
		}
		return nil, fmt.Errorf("read file: %w", err)
	}

	var agents map[string]*Agent
	if err := json.Unmarshal(data, &agents); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	if agents == nil {
		agents = make(map[string]*Agent)
	}

	return agents, nil
}
