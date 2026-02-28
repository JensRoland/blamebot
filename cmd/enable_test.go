package cmd

import (
	"testing"
)

func TestFilterHookEntries(t *testing.T) {
	tests := []struct {
		name    string
		hooks   map[string]interface{}
		key     string
		exclude string
		expect  int // expected number of entries in result
	}{
		{
			"empty hooks",
			map[string]interface{}{},
			"PostToolUse",
			"git-blamebot",
			0,
		},
		{
			"hooks with matching command excluded",
			map[string]interface{}{
				"PostToolUse": []interface{}{
					map[string]interface{}{
						"matcher": "Edit|Write",
						"hooks": []interface{}{
							map[string]interface{}{
								"type":    "command",
								"command": "/usr/local/bin/git-blamebot hook post-tool-use",
							},
						},
					},
				},
			},
			"PostToolUse",
			"git-blamebot",
			0,
		},
		{
			"hooks without matching command preserved",
			map[string]interface{}{
				"PostToolUse": []interface{}{
					map[string]interface{}{
						"matcher": "Edit|Write",
						"hooks": []interface{}{
							map[string]interface{}{
								"type":    "command",
								"command": "/usr/local/bin/other-tool hook post-tool-use",
							},
						},
					},
				},
			},
			"PostToolUse",
			"git-blamebot",
			1,
		},
		{
			"non-map entries preserved",
			map[string]interface{}{
				"PostToolUse": []interface{}{
					"some-string-entry",
					42,
				},
			},
			"PostToolUse",
			"git-blamebot",
			2,
		},
		{
			"mixed entries only matching removed",
			map[string]interface{}{
				"PostToolUse": []interface{}{
					map[string]interface{}{
						"matcher": "Edit|Write",
						"hooks": []interface{}{
							map[string]interface{}{
								"type":    "command",
								"command": "/usr/local/bin/git-blamebot hook post-tool-use",
							},
						},
					},
					map[string]interface{}{
						"matcher": "Edit",
						"hooks": []interface{}{
							map[string]interface{}{
								"type":    "command",
								"command": "/usr/local/bin/other-tool hook post-tool-use",
							},
						},
					},
					"some-string-entry",
				},
			},
			"PostToolUse",
			"git-blamebot",
			2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterHookEntries(tt.hooks, tt.key, tt.exclude)
			if len(got) != tt.expect {
				t.Errorf("filterHookEntries() returned %d entries, want %d; entries: %v", len(got), tt.expect, got)
			}
		})
	}
}
