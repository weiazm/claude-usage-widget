// Package usage fetches Claude usage from the same endpoint the Claude Code TUI
// /usage command uses: GET https://api.anthropic.com/api/oauth/usage.
package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	endpoint    = "https://api.anthropic.com/api/oauth/usage"
	betaHeader  = "oauth-2025-04-20"
	httpTimeout = 15 * time.Second
)

// client is a lean HTTP client tuned for a once-a-minute poll: no idle
// connections are kept around between polls (we only fire a single request every
// 60s, so a persistent connection and its goroutine would just sit idle).
var client = &http.Client{
	Transport: &http.Transport{
		DisableKeepAlives:   true,
		MaxIdleConns:        1,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   false,
	},
}

// ErrUnauthorized indicates a 401 — the token is no longer accepted.
var ErrUnauthorized = fmt.Errorf("unauthorized (401); open Claude Code once to refresh")

// Window is a single rate-limit window (e.g. the 5-hour or 7-day window).
type Window struct {
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at"`
}

// ExtraUsage describes pay-as-you-go overflow usage.
type ExtraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
	Currency     *string  `json:"currency"`
}

// Usage is the parsed response. Per-model weekly windows are pointers because
// they are null on lower subscription tiers.
type Usage struct {
	FiveHour       *Window    `json:"five_hour"`
	SevenDay       *Window    `json:"seven_day"`
	SevenDayOpus   *Window    `json:"seven_day_opus"`
	SevenDaySonnet *Window    `json:"seven_day_sonnet"`
	ExtraUsage     ExtraUsage `json:"extra_usage"`

	// FetchedAt records when this snapshot was retrieved (set by Fetch).
	FetchedAt time.Time `json:"-"`
}

// Fetch retrieves current usage using the given bearer token.
func Fetch(ctx context.Context, accessToken string) (*Usage, error) {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", betaHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("usage endpoint returned HTTP %d", resp.StatusCode)
	}

	var u Usage
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("decode usage response: %w", err)
	}
	u.FetchedAt = time.Now()
	return &u, nil
}
