package providers

import "testing"

func TestTruncateBody(t *testing.T) {
	tests := []struct {
		name   string
		body   []byte
		maxLen int
		want   string
	}{
		{"short body unchanged", []byte("hello"), 10, "hello"},
		{"exact length unchanged", []byte("hello"), 5, "hello"},
		{"long body truncated", []byte("hello world"), 5, "hello..."},
		{"empty body", []byte(""), 10, ""},
		{"very long HTML error", []byte("<html><body>Error 502 Bad Gateway</body></html>"), 20, "<html><body>Error 50..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateBody(tt.body, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateBody() = %q, want %q", got, tt.want)
			}
		})
	}
}
