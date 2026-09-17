package main

// event_fallback_test.go —— 事件静默兜底扫描 回归测试。
//
// 背景：2026-09-18 线上实例——CD2 四个云盘的云端原生事件监听器全部未运行
// （isCloudEventListenerRunning=false），PushMessage 流里只有 LOG_MESSAGE=7 心跳，
// 文件变更事件（FILE_SYSTEM_CHANGE=4）完全断流。旧版（F1 范围过滤之前）任何 FSC 事件都会
// 触发扫描，CD2 自身操作的涓流事件意外充当了高频兜底；F1 精确化后此类实例完全静默——
// 网盘里新出现的垃圾文件无人清理（用户实测：升级 UI 后垃圾不再被删，CD2 与账号均未变动）。
//
// 修复：新增「事件静默兜底扫描」（event_fallback_scan_minutes，默认 15，0=关闭）。
// 本文件覆盖：
//   - fallbackScanDue 纯函数（表驱动：关闭 / 事件新鲜 / 静默超限 / 兜底节流 / 未收到过事件）；
//   - runEventFallbackScanner 循环的最小行为（真实 ctx + 短 ticker 注入不可行，用纯函数覆盖判定，
//     循环体仅做 select + 判定 + 触发，逻辑以判定为准）；
//   - 配置迁移 v2→v3：存量 v2 配置（不含新字段）升级后自动补默认 15 并落盘；
//   - 用户意图尊重：v3+ 显式 0（关闭兜底）不得被改写；
//   - defaultConfig / normalizeConfig 的默认与钳制。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFallbackScanDue(t *testing.T) {
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.Local)
	start := base.Add(-1 * time.Hour) // 订阅建立于 1 小时前
	lastEvent := base.Add(-20 * time.Minute)
	lastFallback := base.Add(-10 * time.Minute)

	cases := []struct {
		name         string
		interval     time.Duration
		lastFile     time.Time // 零值 = 本进程从未收到过文件事件
		lastFallback time.Time
		now          time.Time
		want         bool
	}{
		{"interval为0_关闭兜底", 0, lastEvent, time.Time{}, base, false},
		{"interval为负_关闭兜底", -5 * time.Minute, lastEvent, time.Time{}, base, false},
		{"事件新鲜_不兜底", 15 * time.Minute, base.Add(-5 * time.Minute), time.Time{}, base, false},
		{"事件静默恰好达到阈值_兜底", 15 * time.Minute, base.Add(-15 * time.Minute), time.Time{}, base, true},
		{"事件静默远超阈值_兜底", 15 * time.Minute, lastEvent, time.Time{}, base, true},
		{"刚兜底过_节流跳过", 15 * time.Minute, lastEvent, lastFallback, base, false},
		{"兜底间隔也已超_再次兜底", 15 * time.Minute, lastEvent, base.Add(-20 * time.Minute), base, true},
		{"从未收到事件_从start起算未超_不兜底", 15 * time.Minute, time.Time{}, time.Time{}, start.Add(5 * time.Minute), false},
		{"从未收到事件_从start起算已超_兜底", 15 * time.Minute, time.Time{}, time.Time{}, base, true},
	}
	for _, tc := range cases {
		if got := fallbackScanDue(tc.now, tc.lastFile, tc.lastFallback, start, tc.interval); got != tc.want {
			t.Errorf("%s: fallbackScanDue=%v, 期望 %v", tc.name, got, tc.want)
		}
	}
}

// TestMigrateConfig_V2ToV3_EventFallback 存量 v2 配置（不含新字段，反序列化得 0=关闭兜底）
// 升级时必须被一次性补为默认 15 分钟，否则兜底对存量用户形同不存在（F2 教训重演）。
func TestMigrateConfig_V2ToV3_EventFallback(t *testing.T) {
	// migrateConfig 在迁移时会经 appendLog 写日志；重定向到 t.TempDir() 以免污染 data/clean.log。
	defer withTempPaths(t)()
	got := migrateConfig(Config{ConfigVersion: 2, Token: "tok", Tasks: []string{"/电影"}})
	if got.EventFallbackScanMinutes != 15 {
		t.Fatalf("v2 配置迁移后事件静默兜底应为 15，实际 %d", got.EventFallbackScanMinutes)
	}
	if got.ConfigVersion != currentConfigVersion {
		t.Fatalf("迁移后 config_version=%d，期望 %d", got.ConfigVersion, currentConfigVersion)
	}
	if len(got.Tasks) != 1 || got.Tasks[0] != "/电影" {
		t.Fatalf("迁移不应丢失既有字段，tasks=%v", got.Tasks)
	}

	// 已是最新版本：不得改写（用户显式 0 = 关闭兜底的意图必须保留）。
	keep := migrateConfig(Config{ConfigVersion: currentConfigVersion, EventFallbackScanMinutes: 0})
	if keep.EventFallbackScanMinutes != 0 {
		t.Fatalf("v3+ 显式 0 不得被迁移改写，实际 %d", keep.EventFallbackScanMinutes)
	}

	// 幂等：迁移结果再次迁移不变。
	again := migrateConfig(got)
	if again.EventFallbackScanMinutes != 15 || again.ConfigVersion != currentConfigVersion {
		t.Fatalf("迁移应幂等，再次迁移得 %+v", again)
	}
}

func TestDefaultAndNormalize_EventFallback(t *testing.T) {
	if got := defaultConfig().EventFallbackScanMinutes; got != 15 {
		t.Fatalf("默认事件静默兜底应为 15，实际 %d", got)
	}
	if got := normalizeConfig(Config{EventFallbackScanMinutes: 0}).EventFallbackScanMinutes; got != 0 {
		t.Fatalf("normalizeConfig 不得把 0（关闭兜底）改成默认值，实际 %d", got)
	}
	if got := normalizeConfig(Config{EventFallbackScanMinutes: 30}).EventFallbackScanMinutes; got != 30 {
		t.Fatalf("normalizeConfig 应保留合法值 30，实际 %d", got)
	}
	if got := normalizeConfig(Config{EventFallbackScanMinutes: 99999}).EventFallbackScanMinutes; got != 15 {
		t.Fatalf("非法超大值应回退默认 15，实际 %d", got)
	}
}

// TestMustLoadConfig_MigratesEventFallback 复现存量用户场景：磁盘上的 v2 config.json
// （含 config_version=2、不含 event_fallback_scan_minutes）升级启动时应被一次性迁移为
// 默认 15 并落盘；v3+ 显式 0（关闭兜底）不得被改写。
func TestMustLoadConfig_MigratesEventFallback(t *testing.T) {
	dir := t.TempDir()
	oldCfgPath, oldRecPath, oldLogPath := configPath, recordsPath, logPath
	stateMu.Lock()
	oldCfg := cfg
	stateMu.Unlock()
	configPath = filepath.Join(dir, "config.json")
	recordsPath = filepath.Join(dir, "records.jsonl")
	logPath = filepath.Join(dir, "clean.log")
	defer func() {
		configPath, recordsPath, logPath = oldCfgPath, oldRecPath, oldLogPath
		stateMu.Lock()
		cfg = oldCfg
		stateMu.Unlock()
	}()

	// v2 存量配置：不含 event_fallback_scan_minutes（→0）。升级后应迁移为 15 并落盘。
	v2 := fmt.Sprintf(`{"config_version":2,"event_scan_min_interval_minutes":5,"token":"tok","tasks":["/电影"]}`)
	if err := os.WriteFile(configPath, []byte(v2), 0600); err != nil {
		t.Fatalf("写 v2 配置失败: %v", err)
	}
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	c := currentConfig()
	if c.EventFallbackScanMinutes != 15 {
		t.Fatalf("v2 配置升级后事件静默兜底应为 15，实际 %d", c.EventFallbackScanMinutes)
	}
	if c.ConfigVersion != currentConfigVersion {
		t.Fatalf("迁移后 config_version=%d，期望 %d", c.ConfigVersion, currentConfigVersion)
	}
	// 迁移不得破坏既有字段（含 v1→v2 已迁移过的 event_scan_min_interval_minutes）。
	if c.EventScanMinIntervalMinutes != 5 {
		t.Fatalf("迁移不应改写既有字段 event_scan_min_interval_minutes=5，实际 %d", c.EventScanMinIntervalMinutes)
	}
	if len(c.Tasks) != 1 || c.Tasks[0] != "/电影" {
		t.Fatalf("迁移不应丢失既有字段，tasks=%v", c.Tasks)
	}
	b, _ := os.ReadFile(configPath)
	if want := fmt.Sprintf(`"event_fallback_scan_minutes": %d`, 15); !strings.Contains(string(b), want) {
		t.Fatalf("迁移结果未落盘（应含 %s）：%s", want, string(b))
	}

	// 尊重用户意图：版本已是最新且显式设 0 → 不得改写成 15。
	cur := fmt.Sprintf(`{"config_version":%d,"event_fallback_scan_minutes":0,"token":"tok"}`, currentConfigVersion)
	if err := os.WriteFile(configPath, []byte(cur), 0600); err != nil {
		t.Fatal(err)
	}
	if err := mustLoadConfig(); err != nil {
		t.Fatal(err)
	}
	if got := currentConfig().EventFallbackScanMinutes; got != 0 {
		t.Fatalf("版本已是最新时不得改写用户显式设置的 0（关闭兜底），实际 %d", got)
	}
}
