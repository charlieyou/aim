package providers

import (
	"path/filepath"
)

// CredentialSource indicates where credentials are loaded from
type CredentialSource int

const (
	// SourceProxy indicates credentials from ~/.cli-proxy-api/
	SourceProxy CredentialSource = iota
	// SourceNative indicates credentials from native CLI directories
	SourceNative
)

// DetectCredentialSource checks if ANY provider has creds in ~/.cli-proxy-api/
// Returns SourceProxy if any found, SourceNative if empty or homeDir is invalid
func DetectCredentialSource(homeDir string) CredentialSource {
	// Guard against empty homeDir (e.g., when os.UserHomeDir() fails in CI)
	// to avoid scanning current directory and producing misleading results
	if homeDir == "" {
		return SourceNative
	}

	patterns := []string{
		filepath.Join(homeDir, ".cli-proxy-api", "claude-*.json"),
		filepath.Join(homeDir, ".cli-proxy-api", "codex-*.json"),
		filepath.Join(homeDir, ".cli-proxy-api", "gemini-*.json"),
	}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			return SourceProxy
		}
	}
	return SourceNative
}

// DisplayName returns human-readable source for UI
func (s CredentialSource) DisplayName() string {
	switch s {
	case SourceProxy:
		return "~/.cli-proxy-api/"
	case SourceNative:
		return "native CLI directories"
	}
	return "unknown"
}
