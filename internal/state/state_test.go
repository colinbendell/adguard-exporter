package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s, err := New(stateFile)
	if err != nil {
		t.Fatalf("Failed to create new state: %v", err)
	}

	if s == nil {
		t.Fatal("Expected state to be non-nil")
	}

	if s.LastQueryTime == nil {
		t.Fatal("Expected LastQueryTime map to be initialized")
	}
}

func TestGetSetLastQueryTime(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s, err := New(stateFile)
	if err != nil {
		t.Fatalf("Failed to create new state: %v", err)
	}

	// Test getting non-existent timestamp (should return 0)
	serverURL := "http://example.com"
	timestamp := s.GetLastQueryTime(serverURL)
	if timestamp != 0 {
		t.Errorf("Expected 0 for non-existent server, got %d", timestamp)
	}

	// Test setting and getting timestamp (Unix epoch milliseconds)
	expectedTime := int64(1704110400000) // 2024-01-01T12:00:00Z
	err = s.SetLastQueryTime(serverURL, expectedTime)
	if err != nil {
		t.Fatalf("Failed to set last query time: %v", err)
	}

	timestamp = s.GetLastQueryTime(serverURL)
	if timestamp != expectedTime {
		t.Errorf("Expected timestamp %d, got %d", expectedTime, timestamp)
	}
}

func TestPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	// Create state and set a value
	s1, err := New(stateFile)
	if err != nil {
		t.Fatalf("Failed to create new state: %v", err)
	}

	serverURL := "http://example.com"
	expectedTime := int64(1704110400000) // 2024-01-01T12:00:00Z
	err = s1.SetLastQueryTime(serverURL, expectedTime)
	if err != nil {
		t.Fatalf("Failed to set last query time: %v", err)
	}

	// Create a new state instance and load from the same file
	s2, err := New(stateFile)
	if err != nil {
		t.Fatalf("Failed to load existing state: %v", err)
	}

	// Verify the value persisted
	timestamp := s2.GetLastQueryTime(serverURL)
	if timestamp != expectedTime {
		t.Errorf("Expected timestamp %d after reload, got %d", expectedTime, timestamp)
	}
}

func TestMultipleServers(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s, err := New(stateFile)
	if err != nil {
		t.Fatalf("Failed to create new state: %v", err)
	}

	// Set timestamps for multiple servers (Unix epoch milliseconds)
	servers := map[string]int64{
		"http://server1.com": 1704110400000, // 2024-01-01T12:00:00Z
		"http://server2.com": 1704200400000, // 2024-01-02T13:00:00Z
		"http://server3.com": 1704290400000, // 2024-01-03T14:00:00Z
	}

	for url, timestamp := range servers {
		err = s.SetLastQueryTime(url, timestamp)
		if err != nil {
			t.Fatalf("Failed to set timestamp for %s: %v", url, err)
		}
	}

	// Verify all timestamps
	for url, expectedTime := range servers {
		timestamp := s.GetLastQueryTime(url)
		if timestamp != expectedTime {
			t.Errorf("Expected timestamp %d for %s, got %d", expectedTime, url, timestamp)
		}
	}
}

func TestSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s, err := New(stateFile)
	if err != nil {
		t.Fatalf("Failed to create new state: %v", err)
	}

	// Set a value
	serverURL := "http://example.com"
	expectedTime := int64(1704110400000) // 2024-01-01T12:00:00Z
	err = s.SetLastQueryTime(serverURL, expectedTime)
	if err != nil {
		t.Fatalf("Failed to set last query time: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Fatal("State file was not created")
	}

	// Load the state
	err = s.Load()
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	// Verify the value
	timestamp := s.GetLastQueryTime(serverURL)
	if timestamp != expectedTime {
		t.Errorf("Expected timestamp %d after load, got %d", expectedTime, timestamp)
	}
}

func TestDirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	// Use nested directories that don't exist yet
	stateFile := filepath.Join(tmpDir, "nested", "dirs", "state.json")

	s, err := New(stateFile)
	if err != nil {
		t.Fatalf("Failed to create new state: %v", err)
	}

	// Set a value (this should create directories)
	serverURL := "http://example.com"
	expectedTime := int64(1704110400000) // 2024-01-01T12:00:00Z
	err = s.SetLastQueryTime(serverURL, expectedTime)
	if err != nil {
		t.Fatalf("Failed to set last query time: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Fatal("State file was not created in nested directories")
	}
}
