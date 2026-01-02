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
		// UTF-8 safety tests
		{"utf8 truncate at rune boundary", []byte("hello世界"), 5, "hello..."},
		{"utf8 truncate mid-rune backs off", []byte("hello世界"), 6, "hello..."},   // 世 is 3 bytes, cutting at 6 would split it
		{"utf8 truncate mid-rune backs off 2", []byte("hello世界"), 7, "hello..."}, // still in middle of 世
		{"utf8 truncate includes full rune", []byte("hello世界"), 8, "hello世..."},  // 8 bytes = "hello" + 世
		{"utf8 emoji truncate", []byte("hi🎉bye"), 6, "hi🎉..."},                   // 🎉 is 4 bytes, "hi" + 🎉 = 6 bytes
		{"utf8 emoji mid-truncate", []byte("hi🎉bye"), 3, "hi..."},                // would split emoji
		{"all multibyte", []byte("日本語"), 3, "日..."},                              // each char is 3 bytes
		{"all multibyte mid", []byte("日本語"), 4, "日..."},                          // would split 本
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
