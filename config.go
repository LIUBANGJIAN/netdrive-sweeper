package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config 是单实例的内存配置 + 持久化契约。
type Config struct {
	Address             string   `json:"address"`
	Token               string   `json:"token"`
	AdExts              string   `json:"ad_exts"`
	VideoExts           string   `json:"video_exts"`
	SizeLimitMB         float64  `json:"size_limit_mb"`
	ExcludeDirs         string   `json:"exclude_dirs"`
	MaxDepth            int      `json:"max_depth"`
	ForceRefresh        bool     `json:"force_refresh"`
	OfflineOnly         bool     `json:"offline_only"`
	DeletePermanently   bool     `json:"delete_permanently"`
	AllowDelete         bool     `json:"allow_delete"`
	OpsPerSec           float64  `json:"ops_per_sec"`
	Burst               int      `json:"burst"`
	MaxFilesPerRun      int      `json:"max_files_per_run"`
	MaxTotalBytes       int64    `json:"max_total_bytes"`
	FileCooldownHours   int      `json:"file_cooldown_hours"`
	EnablePush          bool     `json:"enable_push"`
	PushDebounceSeconds int      `json:"push_debounce_seconds"`
	IncompleteSuffixes  string   `json:"incomplete_suffixes"`
	Tasks               []string `json:"tasks"`
}

func defaultConfig() Config {
	return Config{
		Address:             "127.0.0.1:19798",
		AdExts:              ".txt,.html,.url,.lnk",
		VideoExts:           ".mp4,.mkv,.ts",
		SizeLimitMB:         20,
		ExcludeDirs:         "重要,备份",
		MaxDepth:            0,
		ForceRefresh:        false,
		OfflineOnly:         true,
		DeletePermanently:   false, // 默认进网盘回收站（用户拍板）
		AllowDelete:         false, // 删除总开关默认关闭
		OpsPerSec:           5.0,   // 115 官方 maxQueriesPerSecondLimit
		Burst:               10,
		MaxFilesPerRun:      2000,
		MaxTotalBytes:       10 << 30, // 10 GiB
		FileCooldownHours:   6,
		EnablePush:          true,
		PushDebounceSeconds: 5,
		IncompleteSuffixes:  ".part,.download,.!qB,.bc!,.aria2,.crdownload,.td,.tmp,.!ut",
		// Tasks 默认为空：空目录 = 不扫描任何目录（T7 裁决）。
		// 仅影响「新装 / 重置」的默认值；已有配置维持原值（本次不迁移历史数据）。
		Tasks: []string{},
	}
}

func getenv(k, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return fallback
}

func normalizeConfig(c Config) Config {
	c.Address = normalizeAddress(c.Address)
	c.OpsPerSec = clampFloat(c.OpsPerSec, 0.5, 20.0, 5.0)
	c.Burst = clampInt(c.Burst, 1, 50, 10)
	c.MaxFilesPerRun = clampInt(c.MaxFilesPerRun, 1, 100000, 2000)
	c.MaxTotalBytes = clampInt64(c.MaxTotalBytes, 1, 1<<40, 10<<30)
	// max_depth：0 = 不限递归深度（默认）。负数归 0；仅「过大」归 100。
	// 不能用 clampInt(v,0,64,0) 的默认回退——那会把非法大值回退成 0（=不限），方向更危险。
	switch {
	case c.MaxDepth < 0:
		c.MaxDepth = 0
	case c.MaxDepth > 100:
		c.MaxDepth = 100
	}
	c.FileCooldownHours = clampInt(c.FileCooldownHours, 0, 168, 6)
	c.PushDebounceSeconds = clampInt(c.PushDebounceSeconds, 1, 120, 5)
	// 防呆：未完成后缀被清空会静默废掉「含未完成后缀则整目录跳过」这道保险丝（P0-23）。
	if strings.TrimSpace(c.IncompleteSuffixes) == "" {
		c.IncompleteSuffixes = defaultConfig().IncompleteSuffixes
	}
	c.Tasks = cleanTasks(c.Tasks)
	return c
}

func clampFloat(v, lo, hi, def float64) float64 {
	if v < lo || v > hi {
		return def
	}
	return v
}

func clampInt(v, lo, hi, def int) int {
	if v < lo || v > hi {
		return def
	}
	return v
}

func clampInt64(v, lo, hi, def int64) int64 {
	if v < lo || v > hi {
		return def
	}
	return v
}

func mustLoadConfig() error {
	stateMu.Lock()
	defer stateMu.Unlock()
	if b, err := os.ReadFile(configPath); err == nil {
		b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
		var loaded Config
		if err := json.Unmarshal(b, &loaded); err != nil {
			cfg = defaultConfig()
			_ = saveConfigLocked() // 修复损坏的配置文件，避免每次启动重复告警
			return fmt.Errorf("配置文件解析失败，已重置为默认配置: %w", err)
		}
		cfg = normalizeConfig(loaded)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("配置文件读取失败: %w", err)
	} else {
		// 首次启动：落盘完整默认值
		cfg = defaultConfig()
		if err := saveConfigLocked(); err != nil {
			return err
		}
	}
	return ensureDataDirs()
}

func saveConfigLocked() error {
	if err := ensureDataDirs(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	// 轮转 .bak
	if _, err := os.Stat(configPath); err == nil {
		_ = os.Rename(configPath, configPath+".bak")
	}
	if err := os.Rename(tmp, configPath); err != nil {
		return err
	}
	return nil
}

func ensureDataDirs() error {
	for _, p := range []string{configPath, recordsPath, logPath} {
		if dir := filepath.Dir(p); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("创建目录失败 %s: %w", dir, err)
			}
		}
	}
	return nil
}

func currentConfig() Config {
	stateMu.Lock()
	defer stateMu.Unlock()
	return cfg
}

// normalizeAddress 去掉协议前缀、补默认端口。
func normalizeAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.Trim(raw, "/")
	if raw != "" && !strings.Contains(raw, ":") {
		raw += ":19798"
	}
	return raw
}

// normalizePath 统一为以 / 开头的 API 路径。
func normalizePath(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || raw == "/" {
		return "/"
	}
	return "/" + strings.Trim(raw, "/")
}

func displayPath(token *TokenInfo, apiPath string) string {
	root := "/"
	if token != nil && strings.TrimSpace(token.RootDir) != "" {
		root = strings.TrimRight(strings.ReplaceAll(token.RootDir, "\\", "/"), "/")
		if root == "" {
			root = "/"
		}
	}
	apiPath = normalizePath(apiPath)
	if apiPath == "/" {
		return root
	}
	return strings.TrimRight(root, "/") + apiPath
}

func splitCSV(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == ';' || r == '\n' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func contains(xs []string, v string) bool {
	v = strings.ToLower(v)
	for _, x := range xs {
		if strings.ToLower(x) == v {
			return true
		}
	}
	return false
}

func cleanTasks(tasks []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range tasks {
		// 跳过空白项：空串经 normalizePath 会被规范化成 "/"，即 tasks:[""] 被静默放大为
		// 「扫描根目录」——正是 T7 要消灭的危险路径。此处用原始串判空，
		// 以免误伤用户显式填写的 "/"（那是合法的「扫根目录」意图）。
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		t := normalizePath(raw)
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func joinPath(parent, name string) string {
	return normalizePath(strings.TrimRight(parent, "/") + "/" + strings.Trim(name, "/"))
}
