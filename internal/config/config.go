// Package config 读取用户的可选配置文件，让“轮询间隔”“用哪个窗口给图标着色”等行为
// 无需改代码重编即可调整（对应 README 后续优化方向第 2 条）。
//
// 配置文件是 JSON，放在 ~/.claude/claude-usage-widget.json。文件缺失或字段无效时一律
// 退回内置默认值——配置只是“锦上添花”，绝不应该让 widget 起不来。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// IconSource 取值：图标颜色跟随哪个用量窗口。
//
// Go 提示：这里把字符串常量分组声明，纯粹是给 IconSource 字段一组“合法取值”，
// 让代码里比较时不必到处写裸字符串。
const (
	IconSourceFiveHour = "five_hour" // 跟随 5 小时窗口（默认）
	IconSourceSevenDay = "seven_day" // 跟随 7 天窗口
)

const (
	defaultPollSeconds = 60
	minPollSeconds     = 10 // 兜底下限，避免误填过小值疯狂打接口
)

// Config 是解析后的配置。字段都用 JSON 标签映射到文件里的小写下划线键名。
//
// Go 提示：导出字段（首字母大写）才能被 encoding/json 读写；标签把 Go 风格的字段名
// 映射到文件里更口语化的键。
type Config struct {
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
	IconSource          string `json:"icon_source"`
}

// defaults 返回一份内置默认配置。
func defaults() Config {
	return Config{
		PollIntervalSeconds: defaultPollSeconds,
		IconSource:          IconSourceFiveHour,
	}
}

// Path 返回配置文件的完整路径（与凭证同目录 ~/.claude/）。
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "claude-usage-widget.json"), nil
}

// Load 读取并校验配置。文件不存在时写出一份默认模板（方便用户照着改），并返回默认值。
// 任何读取/解析错误都被吞掉，回退到默认值——配置出问题不该影响主功能。
func Load() Config {
	cfg := defaults()

	path, err := Path()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// 文件不存在：写一份默认模板，让用户有东西可改（写失败也无所谓）。
		if os.IsNotExist(err) {
			writeDefault(path, cfg)
		}
		return cfg
	}
	// 解析进一个副本；解析失败就保持默认值不动。
	var parsed Config
	if err := json.Unmarshal(data, &parsed); err != nil {
		return cfg
	}
	if parsed.PollIntervalSeconds > 0 {
		cfg.PollIntervalSeconds = parsed.PollIntervalSeconds
	}
	if parsed.IconSource == IconSourceFiveHour || parsed.IconSource == IconSourceSevenDay {
		cfg.IconSource = parsed.IconSource
	}
	return cfg
}

// PollInterval 把秒数换算成 Duration，并夹到一个安全下限。
//
// Go 提示：方法接收者用值类型 (c Config) 即可，因为我们只读不改。
func (c Config) PollInterval() time.Duration {
	s := c.PollIntervalSeconds
	if s < minPollSeconds {
		s = minPollSeconds
	}
	return time.Duration(s) * time.Second
}

// writeDefault 把一份默认配置写到磁盘（JSON，带缩进便于人工编辑）。出错静默忽略。
func writeDefault(path string, cfg Config) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	// 0644：所有者可读写，其他人只读——普通配置文件的常规权限。
	_ = os.WriteFile(path, data, 0o644)
}
