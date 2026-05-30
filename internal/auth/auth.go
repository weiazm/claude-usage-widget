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

// Go note: package-level "var" with errors.New creates sentinel errors — fixed
// error values the rest of the program can compare against with errors.Is(...).
// That lets callers branch on *which* problem occurred without parsing strings.

// ErrNoCredentials means the credentials file was not found — the user has not
// logged into Claude Code on this machine.
var ErrNoCredentials = errors.New("claude credentials not found; log in with Claude Code first")

// ErrTokenExpired means the stored token is past its expiry. The fix is to open
// Claude Code once so it refreshes the file.
var ErrTokenExpired = errors.New("token expired; open Claude Code once to refresh")

// Token is an access token plus its metadata.
//
// Go note: a "struct" groups related fields. Capitalized field names (AccessToken)
// are exported and visible to other packages; lowercase ones would be private.
type Token struct {
	AccessToken string
	ExpiresAt   time.Time
}

// credentialsFile mirrors the parts of .credentials.json we care about.
//
// Go note: the strings in backticks (`json:"accessToken"`) are "struct tags".
// encoding/json reads them to map JSON keys to Go fields. The nested struct
// matches the nested JSON object {"claudeAiOauth": { ... }}.
type credentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken      string `json:"accessToken"`
		RefreshToken     string `json:"refreshToken"`
		ExpiresAt        int64  `json:"expiresAt"` // epoch milliseconds
		SubscriptionType string `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// CredentialsPath returns the platform credentials file path.
//
// Go note: this returns two values — the path and an error. Returning an error
// as the last value is the standard Go convention.
func CredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	// filepath.Join builds an OS-correct path (backslashes on Windows).
	return filepath.Join(home, ".claude", ".credentials.json"), nil
}

// Read loads and validates the current token from disk.
func Read() (Token, error) {
	path, err := CredentialsPath()
	if err != nil {
		// Go note: Token{} is a zero-valued struct (empty string, zero time). We
		// return it alongside the error; the caller ignores it because err != nil.
		return Token{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Token{}, ErrNoCredentials
		}
		return Token{}, err
	}
	// Go note: declare an empty struct, then pass its address (&c) so Unmarshal
	// can fill it in. "&" takes a pointer; functions use pointers to modify the
	// caller's value instead of a copy.
	var c credentialsFile
	if err := json.Unmarshal(data, &c); err != nil {
		return Token{}, err
	}
	if c.ClaudeAiOauth.AccessToken == "" {
		return Token{}, ErrNoCredentials
	}
	tok := Token{
		AccessToken: c.ClaudeAiOauth.AccessToken,
		// The file stores epoch milliseconds; convert to a time.Time.
		ExpiresAt: time.UnixMilli(c.ClaudeAiOauth.ExpiresAt),
	}
	if c.ClaudeAiOauth.ExpiresAt > 0 && time.Now().After(tok.ExpiresAt) {
		// We still return the (expired) token so a caller could choose to use it,
		// but signal the problem via the error.
		return tok, ErrTokenExpired
	}
	return tok, nil
}
