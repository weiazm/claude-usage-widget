// Package usage 从 Claude Code TUI 里 /usage 命令所用的同一个接口拉取用量数据：
// GET https://api.anthropic.com/api/oauth/usage。
package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Go 提示：用 const (...) 分组可以一次声明多个常量。
const (
	endpoint    = "https://api.anthropic.com/api/oauth/usage"
	betaHeader  = "oauth-2025-04-20"
	httpTimeout = 15 * time.Second
)

// client 是一个为“每分钟轮询一次”场景调优过的精简 HTTP 客户端：轮询之间不保留任何
// 空闲连接（我们每 60 秒只发一个请求，保留长连接及其 goroutine 只会白白空占）。
//
// Go 提示：“&http.Client{...}”创建一个 Client 并返回指向它的指针。我们在包级别建一个
// 共享 client 反复复用——http.Client 可安全并发使用、且会复用连接，所以很少为每次调用
// 单独创建一个。
var client = &http.Client{
	Transport: &http.Transport{
		DisableKeepAlives:   true,
		MaxIdleConns:        1,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   false,
	},
}

// ErrUnauthorized 表示收到 401——token 不再被接受。
var ErrUnauthorized = fmt.Errorf("unauthorized (401); open Claude Code once to refresh")

// Window 表示单个限流窗口（例如 5 小时窗口或 7 天窗口）。
//
// Go 提示：ResetsAt 是一个指向 time.Time 的*指针*。指针可以为 nil，我们用它来表示
// JSON 里缺失/为 null 的值。普通的 time.Time 是不能为 nil 的。
type Window struct {
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at"`
}

// ExtraUsage 描述按量付费的溢出用量。当 JSON 值为 null 时，这些指针字段就是 nil。
type ExtraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
	Currency     *string  `json:"currency"`
}

// Usage 是解析后的响应。按模型细分的周度窗口用指针，因为在较低订阅档位它们是 null。
type Usage struct {
	FiveHour       *Window    `json:"five_hour"`
	SevenDay       *Window    `json:"seven_day"`
	SevenDayOpus   *Window    `json:"seven_day_opus"`
	SevenDaySonnet *Window    `json:"seven_day_sonnet"`
	ExtraUsage     ExtraUsage `json:"extra_usage"`

	// FetchedAt 记录这份快照是什么时候拉取的（由 Fetch 设置）。
	// Go 提示：标签 `json:"-"` 告诉 encoding/json 忽略这个字段，因为它是我们自己加的，
	// 不属于服务端返回的内容。
	FetchedAt time.Time `json:"-"`
}

// Fetch 用给定的 bearer token 拉取当前用量。
//
// Go 提示：返回类型是 (*Usage, error)——返回 Usage 的指针，让调用方共享同一个值（也能
// 和 nil 比较），再加一个 error。失败时返回 (nil, err)；成功时返回 (&u, nil)。
func Fetch(ctx context.Context, accessToken string) (*Usage, error) {
	// 用我们自己的超时包裹调用方传入的 context。WithTimeout 返回一个新 context 和一个
	// cancel 函数；defer cancel() 在函数返回时释放计时器。
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
	// Go 提示：响应 body 是一个必须关闭的流，关闭后才能释放连接。defer 会在 Fetch
	// 返回时（无论走哪条路径）执行 Close。
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("usage endpoint returned HTTP %d", resp.StatusCode)
	}

	var u Usage
	// Decode 借助 json 标签把 JSON body 直接流式解码进我们的结构体。
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		// Go 提示：%w 会“包裹”底层错误，这样我们在补充上下文信息的同时，调用方仍能用
		// errors.Is / errors.As 检查它。
		return nil, fmt.Errorf("decode usage response: %w", err)
	}
	u.FetchedAt = time.Now()
	return &u, nil
}
