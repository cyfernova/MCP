package middleware

import "testing"

func TestNormalizeRequestID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "valid", input: "req_123-abc.DEF", want: "req_123-abc.DEF"},
		{name: "trimmed", input: "  abc-123  ", want: "abc-123"},
		{name: "empty", input: "", want: ""},
		{name: "invalid chars", input: "abc def", want: ""},
		{name: "newline", input: "abc\n123", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeRequestID(tt.input)
			if got != tt.want {
				t.Fatalf("NormalizeRequestID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
