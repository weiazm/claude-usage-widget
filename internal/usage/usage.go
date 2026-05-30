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

// Go note: a grouped "const (...)" block declares several constants at once.
const (
	endpoint    = "https://api.anthropic.com/api/oauth/usage"
	betaHeader  = "oauth-2025-04-20"
	httpTimeout = 15 * time.Second
)

// client is a lean HTTP client tuned for a once-a-minute poll: no idle
// connections are kept around between polls (we only fire a single request every
// 60s, so a persistent connection and its goroutine would just sit idle).
//
// Go note: "&http.Client{...}" creates a Client and gives us a pointer to it.
// We make one shared client at package scope and reuse it — http.Client is safe
// for concurrent use and reuses connections, so you rarely create one per call.
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
//
// Go note: ResetsAt is a *pointer* to time.Time. A pointer can be nil, which we
// use to represent a missing/null JSON value. A plain time.Time can't be nil.
type Window struct {
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at"`
}

// ExtraUsage describes pay-as-you-go overflow usage. The pointer fields are nil
// when the JSON value is null.
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
	// Go note: the tag `json:"-"` tells encoding/json to ignore this field, since
	// it is ours and not part of the server response.
	FetchedAt time.Time `json:"-"`
}

// Fetch retrieves current usage using the given bearer token.
//
// Go note: the return type is (*Usage, error) — a pointer to a Usage so callers
// share one value (and can compare it to nil), plus an error. On failure we
// return (nil, err); on success (&u, nil).
func Fetch(ctx context.Context, accessToken string) (*Usage, error) {
	// Wrap the caller's context with our own timeout. WithTimeout returns a new
	// context and a cancel func; defer cancel() releases the timer on return.
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
	// Go note: the response body is a stream that must be closed to free the
	// connection. defer runs Close when Fetch returns, no matter which path.
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("usage endpoint returned HTTP %d", resp.StatusCode)
	}

	var u Usage
	// Decode streams the JSON body straight into our struct using the json tags.
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		// Go note: %w "wraps" the underlying error so callers can still inspect it
		// with errors.Is / errors.As while we add context to the message.
		return nil, fmt.Errorf("decode usage response: %w", err)
	}
	u.FetchedAt = time.Now()
	return &u, nil
}
