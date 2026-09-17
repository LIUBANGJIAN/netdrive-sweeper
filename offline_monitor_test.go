package main

// offline_monitor_test.go —— 离线任务监控 回归测试。
//
// 背景：2026-09-18 线上实例——CD2 四个云盘的云端原生事件监听器全部未运行，
// 文件事件断流，事件驱动清理失灵，用户禁止重启 CD2。已上线「事件静默兜底扫描」
// （默认 15 分钟）缓解。本功能新增「离线任务监控」：周期性查询清理目录的离线下载状态，
// 检测「下载中→完成」翻转即触发扫描，把响应从 15 分钟缩短到约 1 分钟。
//
// 本文件覆盖：
//   - offlineCompleted 纯函数（表驱动：downloading→finished/error/unknown 均为 true；
//     downloading→downloading、finished→*、unknown→* 均为 false）；
//   - 配置迁移 v3→v4：存量 v3 配置升级后 offline_monitor_minutes==1 且不丢字段、幂等；
//     v4+ 显式 0（关闭监控）不得被改写；
//   - defaultConfig / normalizeConfig 的默认值与钳制；
//   - mustLoadConfig 磁盘 v3 配置升级落盘后含 "offline_monitor_minutes": 1。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineCompleted(t *testing.T) {
	cases := []struct {
		name     string
		old, new string
		want     bool
	}{
		{"downloading转finished_完成", "downloading", "finished", true},
		{"downloading转error_完成", "downloading", "error", true},
		{"downloading转unknown_完成", "downloading", "unknown", true},
		{"downloading转downloading_未完成", "downloading", "downloading", false},
		{"finished转任意_不触发", "finished", "downloading", false},
		{"finished转error_不触发", "finished", "error", false},
		{"unknown转downloading_不触发", "unknown", "downloading", false},
		{"空转downloading_首轮不触发", "", "downloading", false},
		{"空转finished_首轮不触发", "", "finished", false},
	}
	for _, tc := range cases {
		if got := offlineCompleted(tc.old, tc.new); got != tc.want {
			t.Errorf("%s: offlineCompleted(%q,%q)=%v, 期望 %v", tc.name, tc.old, tc.new, got, tc.want)
		}
	}
}

// TestMigrateConfig_V3ToV4_OfflineMonitor 存量 v3 配置（不含新字段，反序列化得 0=关闭监控）
// 升级时必须一次性补为默认 1 分钟，否则离线监控对存量用户形同不存在。
func TestMigrateConfig_V3ToV4_OfflineMonitor(t *testing.T) {
	defer withTempPaths(t)()
	// 模拟真实 v3 配置：经过 v2→v3 迁移后，event_fallback_scan_minutes 已落盘为 15。
	got := migrateConfig(Config{ConfigVersion: 3, EventFallbackScanMinutes: 15, Token: "tok", Tasks: []string{"/电影"}})
	if got.OfflineMonitorMinutes != 1 {
		t.Fatalf("v3 配置迁移后离线监控应为 1，实际 %d", got.OfflineMonitorMinutes)
	}
	if got.ConfigVersion != currentConfigVersion {
		t.Fatalf("迁移后 config_version=%d，期望 %d", got.ConfigVersion, currentConfigVersion)
	}
	if len(got.Tasks) != 1 || got.Tasks[0] != "/电影" {
		t.Fatalf("迁移不应丢失既有字段，tasks=%v", got.Tasks)
	}
	// 迁移不得改写兄弟字段已迁移的值：event_fallback_scan_minutes 应保持 15。
	if got.EventFallbackScanMinutes != 15 {
		t.Fatalf("迁移不应改写既有 event_fallback_scan_minutes=15，实际 %d", got.EventFallbackScanMinutes)
	}

	// 已是最新版本：不得改写（用户显式 0 = 关闭监控的意图必须保留）。
	keep := migrateConfig(Config{ConfigVersion: currentConfigVersion, OfflineMonitorMinutes: 0})
	if keep.OfflineMonitorMinutes != 0 {
		t.Fatalf("v4+ 显式 0 不得被迁移改写，实际 %d", keep.OfflineMonitorMinutes)
	}

	// 幂等：迁移结果再次迁移不变。
	again := migrateConfig(got)
	if again.OfflineMonitorMinutes != 1 || again.ConfigVersion != currentConfigVersion {
		t.Fatalf("迁移应幂等，再次迁移得 %+v", again)
	}
}

func TestDefaultAndNormalize_OfflineMonitor(t *testing.T) {
	if got := defaultConfig().OfflineMonitorMinutes; got != 1 {
		t.Fatalf("默认离线监控应为 1，实际 %d", got)
	}
	if got := normalizeConfig(Config{OfflineMonitorMinutes: 0}).OfflineMonitorMinutes; got != 0 {
		t.Fatalf("normalizeConfig 不得把 0（关闭监控）改成默认值，实际 %d", got)
	}
	if got := normalizeConfig(Config{OfflineMonitorMinutes: 5}).OfflineMonitorMinutes; got != 5 {
		t.Fatalf("normalizeConfig 应保留合法值 5，实际 %d", got)
	}
	if got := normalizeConfig(Config{OfflineMonitorMinutes: 99999}).OfflineMonitorMinutes; got != 1 {
		t.Fatalf("非法超大值应回退默认 1，实际 %d", got)
	}
}

// TestMustLoadConfig_MigratesOfflineMonitor 复现存量用户场景：磁盘上的 v3 config.json
// （含 config_version=3、不含 offline_monitor_minutes）升级启动时应被一次性迁移为默认 1 并落盘；
// v4+ 显式 0（关闭监控）不得被改写。
func TestMustLoadConfig_MigratesOfflineMonitor(t *testing.T) {
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

	// v3 存量配置：不含 offline_monitor_minutes（→0）。升级后应迁移为 1 并落盘。
	v3 := fmt.Sprintf(`{"config_version":3,"event_fallback_scan_minutes":15,"token":"tok","tasks":["/电影"]}`)
	if err := os.WriteFile(configPath, []byte(v3), 0600); err != nil {
		t.Fatalf("写 v3 配置失败: %v", err)
	}
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	c := currentConfig()
	if c.OfflineMonitorMinutes != 1 {
		t.Fatalf("v3 配置升级后离线监控应为 1，实际 %d", c.OfflineMonitorMinutes)
	}
	if c.ConfigVersion != currentConfigVersion {
		t.Fatalf("迁移后 config_version=%d，期望 %d", c.ConfigVersion, currentConfigVersion)
	}
	// 迁移不得破坏既有字段（含 v2→v3 已迁移过的 event_fallback_scan_minutes）。
	if c.EventFallbackScanMinutes != 15 {
		t.Fatalf("迁移不应改写既有字段 event_fallback_scan_minutes=15，实际 %d", c.EventFallbackScanMinutes)
	}
	if len(c.Tasks) != 1 || c.Tasks[0] != "/电影" {
		t.Fatalf("迁移不应丢失既有字段，tasks=%v", c.Tasks)
	}
	b, _ := os.ReadFile(configPath)
	if want := fmt.Sprintf(`"offline_monitor_minutes": %d`, 1); !strings.Contains(string(b), want) {
		t.Fatalf("迁移结果未落盘（应含 %s）：%s", want, string(b))
	}

	// 尊重用户意图：版本已是最新且显式设 0 → 不得改写成 1。
	cur := fmt.Sprintf(`{"config_version":%d,"offline_monitor_minutes":0,"token":"tok"}`, currentConfigVersion)
	if err := os.WriteFile(configPath, []byte(cur), 0600); err != nil {
		t.Fatal(err)
	}
	if err := mustLoadConfig(); err != nil {
		t.Fatal(err)
	}
	if got := currentConfig().OfflineMonitorMinutes; got != 0 {
		t.Fatalf("版本已是最新时不得改写用户显式设置的 0（关闭监控），实际 %d", got)
	}
}
