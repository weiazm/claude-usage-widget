// Package auth reads the Claude Code OAuth token from the local credentials
// file (~/.claude/.credentials.json). Claude Code itself keeps this token fresh
// whenever it is used, so on Windows we can simply read it back each poll.
package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ErrNoCredentials means the credentials file was not found — the user has not
// logged into Claude Code on this machine.
var ErrNoCredentials = errors.New("claude credentials not found; log in with Claude Code first")

// ErrTokenExpired means the stored token is past its expiry. The fix is to open
// Claude Code once so it refreshes the file.
var ErrTokenExpired = errors.New("token expired; open Claude Code once to refresh")

// Token is an access token plus its metadata.
type Token struct {
	AccessToken string
	ExpiresAt   time.Time
}

type credentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken      string `json:"accessToken"`
		RefreshToken     string `json:"refreshToken"`
		ExpiresAt        int64  `json:"expiresAt"` // epoch milliseconds
		SubscriptionType string `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// CredentialsPath returns the platform credentials file path.
func CredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", ".credentials.json"), nil
}

// Read loads and validates the current token from disk.
func Read() (Token, error) {
	path, err := CredentialsPath()
	if err != nil {
		return Token{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Token{}, ErrNoCredentials
		}
		return Token{}, err
	}
	var c credentialsFile
	if err := json.Unmarshal(data, &c); err != nil {
		return Token{}, err
	}
	if c.ClaudeAiOauth.AccessToken == "" {
		return Token{}, ErrNoCredentials
	}
	tok := Token{
		AccessToken: c.ClaudeAiOauth.AccessToken,
		ExpiresAt:   time.UnixMilli(c.ClaudeAiOauth.ExpiresAt),
	}
	if c.ClaudeAiOauth.ExpiresAt > 0 && time.Now().After(tok.ExpiresAt) {
		return tok, ErrTokenExpired
	}
	return tok, nil
}
