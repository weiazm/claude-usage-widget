// Package auth 负责从本机凭证文件（~/.claude/.credentials.json）读取 Claude Code
// 的 OAuth token。Claude Code 每次使用时都会自动刷新这个 token，所以在 Windows 上
// 我们每次轮询时直接把它读出来即可。
package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Go 提示：用包级 var + errors.New 定义“哨兵错误”——固定的错误值，程序其它地方可以
// 用 errors.Is(...) 来比对。这样调用方无需解析字符串就能判断具体是哪种错误。

// ErrNoCredentials 表示凭证文件不存在——用户还没在这台机器上登录过 Claude Code。
var ErrNoCredentials = errors.New("claude credentials not found; log in with Claude Code first")

// ErrTokenExpired 表示存储的 token 已过期。解决办法是打开一次 Claude Code，让它刷新文件。
var ErrTokenExpired = errors.New("token expired; open Claude Code once to refresh")

// Token 表示一个访问令牌及其元数据。
//
// Go 提示：struct（结构体）把相关字段聚到一起。首字母大写的字段名（AccessToken）是
// 导出的，其它包可见；首字母小写则是私有的。
type Token struct {
	AccessToken string
	ExpiresAt   time.Time
}

// credentialsFile 只镜像 .credentials.json 里我们关心的那部分字段。
//
// Go 提示：反引号里的字符串（`json:"accessToken"`）叫“结构体标签”（struct tag）。
// encoding/json 读取它来把 JSON 的键映射到 Go 字段。嵌套结构体对应嵌套的 JSON 对象
// {"claudeAiOauth": { ... }}。
type credentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken      string `json:"accessToken"`
		RefreshToken     string `json:"refreshToken"`
		ExpiresAt        int64  `json:"expiresAt"` // Unix 毫秒时间戳
		SubscriptionType string `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// CredentialsPath 返回当前平台上的凭证文件路径。
//
// Go 提示：这里返回两个值——路径和一个 error。把 error 作为最后一个返回值是 Go 的惯例。
func CredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	// filepath.Join 会拼出符合当前系统的路径（Windows 上用反斜杠）。
	return filepath.Join(home, ".claude", ".credentials.json"), nil
}

// Read 从磁盘加载并校验当前 token。
func Read() (Token, error) {
	path, err := CredentialsPath()
	if err != nil {
		// Go 提示：Token{} 是零值结构体（空字符串、零值时间）。我们把它和 error 一起返回；
		// 由于 err != nil，调用方会忽略这个零值。
		return Token{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Token{}, ErrNoCredentials
		}
		return Token{}, err
	}
	// Go 提示：先声明一个空结构体，再把它的地址（&c）传进去，让 Unmarshal 填充它。
	// “&”取地址（指针）；函数用指针来修改调用方的值，而不是改一个副本。
	var c credentialsFile
	if err := json.Unmarshal(data, &c); err != nil {
		return Token{}, err
	}
	if c.ClaudeAiOauth.AccessToken == "" {
		return Token{}, ErrNoCredentials
	}
	tok := Token{
		AccessToken: c.ClaudeAiOauth.AccessToken,
		// 文件里存的是 Unix 毫秒时间戳；这里转换成 time.Time。
		ExpiresAt: time.UnixMilli(c.ClaudeAiOauth.ExpiresAt),
	}
	if c.ClaudeAiOauth.ExpiresAt > 0 && time.Now().After(tok.ExpiresAt) {
		// 我们仍然把（已过期的）token 返回，让调用方可以选择是否使用它，
		// 但通过 error 把问题信号传出去。
		return tok, ErrTokenExpired
	}
	return tok, nil
}
