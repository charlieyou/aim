package providers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectCredentialSource_ProxyExists(t *testing.T) {
	tmpDir := t.TempDir()
	proxyDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(proxyDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create a claude credential file
	if err := os.WriteFile(filepath.Join(proxyDir, "claude-test.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	source := DetectCredentialSource(tmpDir)
	if source != SourceProxy {
		t.Errorf("expected SourceProxy, got %v", source)
	}
	if source.DisplayName() != "~/.cli-proxy-api/" {
		t.Errorf("expected '~/.cli-proxy-api/', got %q", source.DisplayName())
	}
}

func TestDetectCredentialSource_ProxyEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	// Create empty proxy dir
	proxyDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(proxyDir, 0755); err != nil {
		t.Fatal(err)
	}

	source := DetectCredentialSource(tmpDir)
	if source != SourceNative {
		t.Errorf("expected SourceNative, got %v", source)
	}
	if source.DisplayName() != "native CLI directories" {
		t.Errorf("expected 'native CLI directories', got %q", source.DisplayName())
	}
}

func TestDetectCredentialSource_ProxyDirMissing(t *testing.T) {
	tmpDir := t.TempDir()
	// No proxy dir at all

	source := DetectCredentialSource(tmpDir)
	if source != SourceNative {
		t.Errorf("expected SourceNative, got %v", source)
	}
}

func TestDetectCredentialSource_MixedProviders(t *testing.T) {
	// Test that ANY proxy credential triggers SourceProxy
	tests := []struct {
		name     string
		filename string
	}{
		{"claude", "claude-test.json"},
		{"codex", "codex-test.json"},
		{"gemini", "gemini-test.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			proxyDir := filepath.Join(tmpDir, ".cli-proxy-api")
			if err := os.MkdirAll(proxyDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(proxyDir, tt.filename), []byte("{}"), 0644); err != nil {
				t.Fatal(err)
			}

			source := DetectCredentialSource(tmpDir)
			if source != SourceProxy {
				t.Errorf("expected SourceProxy for %s, got %v", tt.filename, source)
			}
		})
	}
}

func TestCredentialSource_DisplayName_Unknown(t *testing.T) {
	// Test unknown source value
	source := CredentialSource(99)
	if source.DisplayName() != "unknown" {
		t.Errorf("expected 'unknown', got %q", source.DisplayName())
	}
}

func TestDetectCredentialSource_EmptyHomeDir(t *testing.T) {
	// When homeDir is empty (e.g., os.UserHomeDir() fails in CI),
	// should return SourceNative to avoid scanning current directory
	source := DetectCredentialSource("")
	if source != SourceNative {
		t.Errorf("expected SourceNative for empty homeDir, got %v", source)
	}
}
