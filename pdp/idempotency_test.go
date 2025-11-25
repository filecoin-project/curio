package pdp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIdempotencyKeyUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		expectError bool
		errorMsg    string
		expected    string
	}{
		{
			name:     "valid UUID",
			json:     `"550e8400-e29b-41d4-a716-446655440000"`,
			expected: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:     "valid ULID",
			json:     `"01H8XKZ9N8J8R8KZ9N8J8R8K"`,
			expected: "01H8XKZ9N8J8R8KZ9N8J8R8K",
		},
		{
			name:     "valid custom format",
			json:     `"custom-key_123"`,
			expected: "custom-key_123",
		},
		{
			name:     "empty string",
			json:     `""`,
			expected: "",
		},
		{
			name:     "null value",
			json:     `null`,
			expected: "",
		},
		{
			name:        "invalid characters",
			json:        `"invalid@key"`,
			expectError: true,
			errorMsg:    "idempotency key can only contain letters, numbers, hyphens, and underscores",
		},
		{
			name:        "too long key",
			json:        `"` + strings.Repeat("a", 256) + `"`,
			expectError: true,
			errorMsg:    "idempotency key must be 255 characters or less",
		},
		{
			name:        "invalid JSON",
			json:        `"unclosed`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ik IdempotencyKey
			err := json.Unmarshal([]byte(tt.json), &ik)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errorMsg != "" && err.Error() != tt.errorMsg {
					t.Errorf("expected error message %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if string(ik) != tt.expected {
					t.Errorf("expected %q, got %q", tt.expected, string(ik))
				}
			}
		})
	}
}

func TestIdempotencyKeyInStruct(t *testing.T) {
	type TestStruct struct {
		IdempotencyKey IdempotencyKey `json:"idempotencyKey,omitempty"`
		OtherField     string         `json:"otherField"`
	}

	tests := []struct {
		name        string
		json        string
		expectError bool
		expectedKey string
	}{
		{
			name:        "valid key in struct",
			json:        `{"idempotencyKey":"test-key-123","otherField":"value"}`,
			expectedKey: "test-key-123",
		},
		{
			name:        "omitted key",
			json:        `{"otherField":"value"}`,
			expectedKey: "",
		},
		{
			name:        "null key",
			json:        `{"idempotencyKey":null,"otherField":"value"}`,
			expectedKey: "",
		},
		{
			name:        "empty key",
			json:        `{"idempotencyKey":"","otherField":"value"}`,
			expectedKey: "",
		},
		{
			name:        "invalid key in struct",
			json:        `{"idempotencyKey":"invalid@key","otherField":"value"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ts TestStruct
			err := json.Unmarshal([]byte(tt.json), &ts)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if string(ts.IdempotencyKey) != tt.expectedKey {
					t.Errorf("expected key %q, got %q", tt.expectedKey, string(ts.IdempotencyKey))
				}
			}
		})
	}
}
