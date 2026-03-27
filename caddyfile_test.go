package storageredis

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressionModeUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected CompressionMode
		wantErr  bool
	}{
		// Legacy boolean form
		{name: "legacy true → flate", input: `true`, expected: CompressionFlate},
		{name: "legacy false → none", input: `false`, expected: CompressionNone},
		// JSON null is treated as false (Go zero value for bool)
		{name: "null → none", input: `null`, expected: CompressionNone},
		// String form — raw value stored; finalizeConfiguration normalises "false" → CompressionNone
		{name: "string flate", input: `"flate"`, expected: CompressionFlate},
		{name: "string zlib", input: `"zlib"`, expected: CompressionZlib},
		{name: "string false stored as-is", input: `"false"`, expected: CompressionMode("false")},
		{name: "empty string → none", input: `""`, expected: CompressionNone},
		// Invalid
		{name: "number rejected", input: `1`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var c CompressionMode
			err := json.Unmarshal([]byte(tc.input), &c)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, c)
		})
	}
}

func TestDBIndexUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected DBIndex
		wantErr  bool
	}{
		// Legacy integer form
		{name: "int 0", input: `0`, expected: DBIndex("0")},
		{name: "int 9", input: `9`, expected: DBIndex("9")},
		// String form
		{name: "string 0", input: `"0"`, expected: DBIndex("0")},
		{name: "string 9", input: `"9"`, expected: DBIndex("9")},
		// JSON null unmarshals as 0 into int (Go zero value)
		{name: "null → 0", input: `null`, expected: DBIndex("0")},
		// Invalid
		{name: "bool rejected", input: `true`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var d DBIndex
			err := json.Unmarshal([]byte(tc.input), &d)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, d)
		})
	}
}

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
