package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// State holds the persistent state for the exporter
type State struct {
	// LastQueryTime maps server URL to the last query timestamp processed (Unix epoch milliseconds)
	LastQueryTime map[string]int64 `json:"last_query_time"`
	mu            sync.RWMutex
	filePath      string
}

// New creates a new State instance with the given file path
func New(filePath string) (*State, error) {
	s := &State{
		LastQueryTime: make(map[string]int64),
		filePath:      filePath,
	}

	// Try to load existing state
	if err := s.Load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return s, nil
}

// GetLastQueryTime returns the last query time for a given server as Unix epoch milliseconds
// Returns 0 if no timestamp exists (zero value)
func (s *State) GetLastQueryTime(serverURL string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastQueryTime[serverURL]
}

// SetLastQueryTime sets the last query time for a given server (Unix epoch milliseconds)
func (s *State) SetLastQueryTime(serverURL string, epochMillis int64) error {
	s.mu.Lock()
	s.LastQueryTime[serverURL] = epochMillis
	s.mu.Unlock()

	return s.Save()
}

// Load reads the state from disk
func (s *State) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, s)
}

// Save writes the state to disk
func (s *State) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create directory if it doesn't exist
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0644)
}
