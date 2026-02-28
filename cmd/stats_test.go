package cmd

import (
	"database/sql"
	"testing"
)

func TestNullStr(t *testing.T) {
	tests := []struct {
		name   string
		input  sql.NullString
		expect string
	}{
		{
			"valid string",
			sql.NullString{String: "2025-01-01T00:00:00Z", Valid: true},
			"2025-01-01T00:00:00Z",
		},
		{
			"invalid null string",
			sql.NullString{String: "", Valid: false},
			"n/a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullStr(tt.input)
			if got != tt.expect {
				t.Errorf("nullStr(%v) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}
