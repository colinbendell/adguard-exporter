package adguard

import (
	"testing"
)

func TestFilterByTimestamp(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name          string
		entries       []logEntry
		lastQueryTime string
		expectedLen   int
	}{
		{
			name: "empty lastQueryTime returns all entries",
			entries: []logEntry{
				{Time: "2024-01-01T12:00:00Z"},
				{Time: "2024-01-01T13:00:00Z"},
			},
			lastQueryTime: "",
			expectedLen:   2,
		},
		{
			name: "filters entries before lastQueryTime",
			entries: []logEntry{
				{Time: "2024-01-01T12:00:00Z"},
				{Time: "2024-01-01T13:00:00Z"},
				{Time: "2024-01-01T14:00:00Z"},
			},
			lastQueryTime: "2024-01-01T12:30:00Z",
			expectedLen:   2, // Only 13:00 and 14:00 should remain
		},
		{
			name: "all entries before lastQueryTime",
			entries: []logEntry{
				{Time: "2024-01-01T12:00:00Z"},
				{Time: "2024-01-01T13:00:00Z"},
			},
			lastQueryTime: "2024-01-01T15:00:00Z",
			expectedLen:   0,
		},
		{
			name: "all entries after lastQueryTime",
			entries: []logEntry{
				{Time: "2024-01-01T12:00:00Z"},
				{Time: "2024-01-01T13:00:00Z"},
			},
			lastQueryTime: "2024-01-01T11:00:00Z",
			expectedLen:   2,
		},
		{
			name:          "empty entries",
			entries:       []logEntry{},
			lastQueryTime: "2024-01-01T12:00:00Z",
			expectedLen:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert lastQueryTime to epoch for the updated API
			var lastQueryEpoch int64
			if tt.lastQueryTime != "" {
				var err error
				lastQueryEpoch, err = rfc3339ToEpoch(tt.lastQueryTime)
				if err != nil {
					t.Fatalf("Failed to convert lastQueryTime: %v", err)
				}
			}

			filtered := client.filterByTimestamp(tt.entries, lastQueryEpoch)
			if len(filtered) != tt.expectedLen {
				t.Errorf("Expected %d entries, got %d", tt.expectedLen, len(filtered))
			}

			// Verify all filtered entries are after lastQueryTime
			if tt.lastQueryTime != "" {
				for _, entry := range filtered {
					entryEpoch, _ := rfc3339ToEpoch(entry.Time)
					if entryEpoch <= lastQueryEpoch {
						t.Errorf("Entry time %s (epoch:%d) should be after %s (epoch:%d)",
							entry.Time, entryEpoch, tt.lastQueryTime, lastQueryEpoch)
					}
				}
			}
		})
	}
}

func TestGetLatestTimestamp(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name         string
		entries      []logEntry
		expectedTime string
	}{
		{
			name: "returns latest timestamp",
			entries: []logEntry{
				{Time: "2024-01-01T12:00:00Z"},
				{Time: "2024-01-01T15:00:00Z"},
				{Time: "2024-01-01T13:00:00Z"},
			},
			expectedTime: "2024-01-01T15:00:00Z",
		},
		{
			name: "single entry",
			entries: []logEntry{
				{Time: "2024-01-01T12:00:00Z"},
			},
			expectedTime: "2024-01-01T12:00:00Z",
		},
		{
			name:         "empty entries",
			entries:      []logEntry{},
			expectedTime: "",
		},
		{
			name: "entries in reverse order",
			entries: []logEntry{
				{Time: "2024-01-01T15:00:00Z"},
				{Time: "2024-01-01T14:00:00Z"},
				{Time: "2024-01-01T13:00:00Z"},
			},
			expectedTime: "2024-01-01T15:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultEpoch := client.getLatestTimestamp(tt.entries)

			// Convert expected time to epoch for comparison
			var expectedEpoch int64
			if tt.expectedTime != "" {
				var err error
				expectedEpoch, err = rfc3339ToEpoch(tt.expectedTime)
				if err != nil {
					t.Fatalf("Failed to convert expected time: %v", err)
				}
			}

			if resultEpoch != expectedEpoch {
				t.Errorf("Expected epoch %d (%s), got %d", expectedEpoch, tt.expectedTime, resultEpoch)
			}
		})
	}
}

func TestTimestampComparison(t *testing.T) {
	// Test that RFC3339 timestamps can be compared as strings
	tests := []struct {
		time1    string
		time2    string
		expected string // which one should be "greater"
	}{
		{
			time1:    "2024-01-01T12:00:00Z",
			time2:    "2024-01-01T13:00:00Z",
			expected: "time2",
		},
		{
			time1:    "2024-01-01T12:00:00Z",
			time2:    "2024-01-02T12:00:00Z",
			expected: "time2",
		},
		{
			time1:    "2024-01-01T12:00:00Z",
			time2:    "2024-01-01T12:00:00Z",
			expected: "equal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.time1+" vs "+tt.time2, func(t *testing.T) {
			var result string
			if tt.time1 > tt.time2 {
				result = "time1"
			} else if tt.time1 < tt.time2 {
				result = "time2"
			} else {
				result = "equal"
			}

			if result != tt.expected {
				t.Errorf("Expected %s to be greater, got %s", tt.expected, result)
			}
		})
	}
}

func TestPaginationLogic(t *testing.T) {
	tests := []struct {
		name          string
		oldest        string
		lastQueryTime string
		shouldFetch   bool
		description   string
	}{
		{
			name:          "gap exists - oldest > lastQueryTime",
			oldest:        "2024-01-01T14:00:00Z",
			lastQueryTime: "2024-01-01T12:00:00Z",
			shouldFetch:   true,
			description:   "Should fetch more when oldest is newer than lastQueryTime",
		},
		{
			name:          "no gap - oldest < lastQueryTime",
			oldest:        "2024-01-01T11:00:00Z",
			lastQueryTime: "2024-01-01T12:00:00Z",
			shouldFetch:   false,
			description:   "Should stop when oldest is older than lastQueryTime",
		},
		{
			name:          "no gap - oldest == lastQueryTime",
			oldest:        "2024-01-01T12:00:00Z",
			lastQueryTime: "2024-01-01T12:00:00Z",
			shouldFetch:   false,
			description:   "Should stop when oldest equals lastQueryTime",
		},
		{
			name:          "empty lastQueryTime",
			oldest:        "2024-01-01T12:00:00Z",
			lastQueryTime: "",
			shouldFetch:   false,
			description:   "Should not paginate when lastQueryTime is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the pagination condition
			shouldFetch := tt.lastQueryTime != "" && tt.oldest != "" && tt.oldest > tt.lastQueryTime

			if shouldFetch != tt.shouldFetch {
				t.Errorf("%s: expected shouldFetch=%v, got %v", tt.description, tt.shouldFetch, shouldFetch)
			}

			t.Logf("%s: oldest=%s, lastQueryTime=%s, shouldFetch=%v",
				tt.description, tt.oldest, tt.lastQueryTime, shouldFetch)
		})
	}
}

func TestFilterPreservesDuplicates(t *testing.T) {
	// Verify that filtering doesn't accidentally remove legitimate duplicate timestamps
	client := &Client{}

	entries := []logEntry{
		{Time: "2024-01-01T12:00:00Z", Client: "client1"},
		{Time: "2024-01-01T12:00:00Z", Client: "client2"}, // Same timestamp, different client
		{Time: "2024-01-01T13:00:00Z", Client: "client1"},
	}

	lastQueryEpoch, _ := rfc3339ToEpoch("2024-01-01T11:00:00Z")
	filtered := client.filterByTimestamp(entries, lastQueryEpoch)

	if len(filtered) != 3 {
		t.Errorf("Expected 3 entries after filtering, got %d", len(filtered))
	}
}

func TestRFC3339ToEpoch(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedEpoch int64
		shouldError   bool
	}{
		{
			name:          "valid RFC3339 timestamp",
			input:         "2024-01-01T12:00:00Z",
			expectedEpoch: 1704110400000, // milliseconds
			shouldError:   false,
		},
		{
			name:          "RFC3339 with milliseconds",
			input:         "2024-01-01T12:00:00.123Z",
			expectedEpoch: 1704110400123,
			shouldError:   false,
		},
		{
			name:          "RFC3339 with timezone",
			input:         "2024-01-01T12:00:00+05:00",
			expectedEpoch: 1704092400000, // Adjusted for timezone
			shouldError:   false,
		},
		{
			name:        "invalid timestamp",
			input:       "not-a-timestamp",
			shouldError: true,
		},
		{
			name:        "empty string",
			input:       "",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			epoch, err := rfc3339ToEpoch(tt.input)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error for input %q, got none", tt.input)
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if epoch != tt.expectedEpoch {
					t.Errorf("Expected epoch %d, got %d", tt.expectedEpoch, epoch)
				}
			}
		})
	}
}

func TestEpochToRFC3339(t *testing.T) {
	tests := []struct {
		name     string
		epoch    int64
		expected string
	}{
		{
			name:     "epoch to RFC3339",
			epoch:    1704110400000,
			expected: "2024-01-01T12:00:00Z",
		},
		{
			name:     "epoch with milliseconds",
			epoch:    1704110400123,
			expected: "2024-01-01T12:00:00.123Z",
		},
		{
			name:     "zero epoch",
			epoch:    0,
			expected: "1970-01-01T00:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := epochToRFC3339(tt.epoch)

			// Parse both to compare (handles different nano precision)
			resultTime, err := rfc3339ToEpoch(result)
			if err != nil {
				t.Fatalf("Failed to parse result: %v", err)
			}

			expectedTime, err := rfc3339ToEpoch(tt.expected)
			if err != nil {
				t.Fatalf("Failed to parse expected: %v", err)
			}

			if resultTime != expectedTime {
				t.Errorf("Expected %s (epoch:%d), got %s (epoch:%d)",
					tt.expected, expectedTime, result, resultTime)
			}
		})
	}
}

func TestEpochComparison(t *testing.T) {
	// Test that epoch comparisons work correctly
	time1, _ := rfc3339ToEpoch("2024-01-01T12:00:00Z")
	time2, _ := rfc3339ToEpoch("2024-01-01T13:00:00Z")
	time3, _ := rfc3339ToEpoch("2024-01-01T12:00:00.001Z")

	if time1 >= time2 {
		t.Errorf("12:00 should be before 13:00")
	}

	if time1 >= time3 {
		t.Errorf("12:00:00 should be before 12:00:00.001")
	}

	if time3 >= time2 {
		t.Errorf("12:00:00.001 should be before 13:00:00")
	}
}
