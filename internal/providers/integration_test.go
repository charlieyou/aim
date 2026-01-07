//go:build integration
// +build integration

package providers

import (
    "context"
    "io"
    "net/http"
    "testing"
)

func TestClaudeIntegration(t *testing.T) {    
    p, err := NewClaudeProvider()
    if err != nil {
        t.Fatalf("Constructor error: %v", err)
    }
    
    accounts, err := p.loadCredentials()
    if err != nil {
        t.Fatalf("loadCredentials error: %v", err)
    }
    
    t.Logf("Testing %d accounts", len(accounts))
    
    for i, a := range accounts {
        t.Logf("Account[%d] %s: token=%d chars", i, a.Email, len(a.AccessToken))
        
        // Make raw request to see response
        url := "https://api.anthropic.com/api/oauth/usage"
        req, _ := http.NewRequestWithContext(context.Background(), "GET", url, nil)
        req.Header.Set("Authorization", "Bearer "+a.AccessToken)
        req.Header.Set("Accept", "application/json")
        req.Header.Set("User-Agent", UserAgent())
        req.Header.Set("anthropic-beta", "oauth-2025-04-20")
        
        client := &http.Client{}
        resp, err := client.Do(req)
        if err != nil {
            t.Logf("  Request error: %v", err)
            continue
        }
        
        body, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        
        t.Logf("  Status: %d", resp.StatusCode)
        t.Logf("  Body: %s", string(body))
    }
}
