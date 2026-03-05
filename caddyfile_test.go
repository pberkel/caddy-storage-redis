package storageredis

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeKeyPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "empty string allowed",
			input:    "",
			expected: "",
			wantErr:  false,
		},
		{
			name:     "whitespace only becomes empty and allowed",
			input:    "  ",
			expected: "",
			wantErr:  false,
		},
		{
			name:     "single prefix remains unchanged",
			input:    "caddy",
			expected: "caddy",
			wantErr:  false,
		},
		{
			name:     "surrounding slashes are trimmed",
			input:    "/caddy/",
			expected: "caddy",
			wantErr:  false,
		},
		{
			name:     "nested valid prefix remains unchanged",
			input:    "a/b",
			expected: "a/b",
			wantErr:  false,
		},
		{
			name:    "single dot rejected",
			input:   ".",
			wantErr: true,
		},
		{
			name:    "double dot rejected",
			input:   "..",
			wantErr: true,
		},
		{
			name:    "empty segment rejected",
			input:   "a//b",
			wantErr: true,
		},
		{
			name:    "dot dot segment rejected",
			input:   "a/../b",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			actual, err := normalizeKeyPrefix(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}
