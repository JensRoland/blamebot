package llm

import (
	"testing"
)

func TestFilterEnv(t *testing.T) {
	tests := []struct {
		name     string
		env      []string
		exclude  string
		expected []string
	}{
		{
			name:     "removes_matching_entries",
			env:      []string{"PATH=/usr/bin", "CLAUDECODE=abc123", "HOME=/home/user"},
			exclude:  "CLAUDECODE",
			expected: []string{"PATH=/usr/bin", "HOME=/home/user"},
		},
		{
			name:     "preserves_non_matching_entries",
			env:      []string{"PATH=/usr/bin", "HOME=/home/user", "GOPATH=/go"},
			exclude:  "CLAUDECODE",
			expected: []string{"PATH=/usr/bin", "HOME=/home/user", "GOPATH=/go"},
		},
		{
			name:     "empty_env_slice",
			env:      []string{},
			exclude:  "CLAUDECODE",
			expected: nil,
		},
		{
			name:     "no_matches",
			env:      []string{"FOO=bar", "BAZ=qux"},
			exclude:  "CLAUDECODE",
			expected: []string{"FOO=bar", "BAZ=qux"},
		},
		{
			name:     "prefix_must_match_exactly_with_equals",
			env:      []string{"CLAUDECODER=x", "CLAUDECODE=y"},
			exclude:  "CLAUDECODE",
			expected: []string{"CLAUDECODER=x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterEnv(tt.env, tt.exclude)

			if len(got) != len(tt.expected) {
				t.Fatalf("filterEnv() returned %d entries, want %d\ngot:  %v\nwant: %v",
					len(got), len(tt.expected), got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("filterEnv()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}
