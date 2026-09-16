package main

// event_scope_test.go —— F1 路径范围过滤 / F2 扫描冷却 / F3 日志降噪 回归测试。
//
// 背景：CD2 的 PushMessage 是全局流，别的应用/系统的变更也会推来；旧实现下任何
// FILE_SYSTEM_CHANGE 都会进入防抖并触发全目录扫描，稳定涓流会被刷成每几秒一次。
// 本文件的断言覆盖：
//   - eventInCleanScope 纯函数（表驱动，含段对齐 / 根为 / / 空路径 / 反斜杠 / 多余斜杠）；
//   - onEvent 只对范围内事件触发扫描；范围外/无路径不触发，但仍是订阅存活证据；
//   - 事件驱动扫描最小间隔（冷却）：冷却期内不重复扫描，冷却结束的末尾触发仍执行。

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// withCleanScope 在测试期间把全局配置的清理目录与 Token 根设为给定值，测试后恢复。
// 只读内存，不触网。
func withCleanScope(t *testing.T, tasks []string, root string) {
	t.Helper()
	stateMu.Lock()
	oldCfg := cfg
	oldStatus := statusInfo
	cfg.Tasks = append([]string(nil), tasks...)
	statusInfo.Token = &TokenInfo{RootDir: root}
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		cfg = oldCfg
		statusInfo = oldStatus
		stateMu.Unlock()
	})
}

// ==================== F1：eventInCleanScope 表驱动 ====================

func TestEventInCleanScope(t *testing.T) {
	const root = "/BON_115网盘"
	cases := []struct {
		name   string
		evPath string
		tasks  []string
		root   string
		want   bool
	}{
		{"命中根下子目录", "/BON_115网盘/私存入库/a/b.txt", []string{"/私存入库"}, root, true},
		{"根+task 恰好相等", "/BON_115网盘/私存入库", []string{"/私存入库"}, root, true},
		{"未命中其它目录", "/BON_115网盘/影视/x.mkv", []string{"/私存入库"}, root, false},
		{"段对齐：/影视 不匹配 /影视2", "/BON_115网盘/影视2/x", []string{"/影视"}, root, false},
		{"段对齐：/影视 命中 /影视", "/BON_115网盘/影视/x", []string{"/影视"}, root, true},
		{"根为 / 时直接匹配", "/电影/a.txt", []string{"/电影"}, "/", true},
		{"根为空（视为 /）匹配", "/电影/a.txt", []string{"/电影"}, "", true},
		{"tasks 为空 → false", "/BON_115网盘/私存入库/a", nil, root, false},
		{"evPath 为空 → false", "", []string{"/私存入库"}, root, false},
		{"evPath 全空白 → false", "   ", []string{"/私存入库"}, root, false},
		{"Windows 反斜杠规范化", `\BON_115网盘\私存入库\a.txt`, []string{"/私存入库"}, root, true},
		{"多余斜杠规范化", "//BON_115网盘//私存入库//a.txt", []string{"/私存入库"}, root, true},
		{"尾斜杠规范化", "/BON_115网盘/私存入库/", []string{"/私存入库"}, root, true},
		{"root 无前导斜杠写法", "/BON_115网盘/私存入库/a", []string{"/私存入库"}, "BON_115网盘", true},
		{"task==/ 视为全命中", "/BON_115网盘/任意/x", []string{"/"}, root, true},
		{"根目录本身不属于子 task", "/BON_115网盘", []string{"/私存入库"}, root, false},
		{"ev 已是根相对仍按 task 匹配", "/私存入库/a", []string{"/私存入库"}, root, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eventInCleanScope(c.evPath, c.tasks, c.root); got != c.want {
				t.Fatalf("eventInCleanScope(%q, %v, %q)=%v，期望 %v", c.evPath, c.tasks, c.root, got, c.want)
			}
		})
	}
}

// ==================== F1：onEvent 只对范围内事件触发 ====================

func TestOnEvent_ScopeFilter(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)
	withCleanScope(t, []string{"/私存入库"}, "/BON_115网盘")

	var n int64
	p := newPushConsumer(nil, 20*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
	p.log = func(string, ...any) {}

	// 范围外事件：不应触发扫描。
	p.onEvent(PushEvent{Type: 4, Path: "/BON_115网盘/影视/x.mkv"})
	time.Sleep(10 * p.debounce)
	if got := atomic.LoadInt64(&n); got != 0 {
		t.Fatalf("范围外事件不应触发扫描，实际 %d 次", got)
	}
	if pushSnapshot().LastMessageAt == "" {
		t.Fatal("范围外事件的到达也应刷新 LastMessageAt（订阅存活证据）")
	}
	if ig := pushSnapshot().IgnoredEvents; ig < 1 {
		t.Fatalf("范围外事件应计入 ignoredEvents，实际 %d", ig)
	}

	// 范围内事件：应在防抖后触发。
	p.onEvent(PushEvent{Type: 4, Path: "/BON_115网盘/私存入库/a/b.txt"})
	waitUntil(t, func() bool { return atomic.LoadInt64(&n) >= 1 }, time.Second, "范围内事件应在防抖后触发扫描")
	if pushSnapshot().LastEventPath == "" {
		t.Fatal("范围内事件应更新最近事件路径（lastEventPath）")
	}
}

// ==================== F1b：路径为空 → 不触发 + 节流记录 ====================

func TestOnEvent_PathlessCountsIgnored(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)
	withCleanScope(t, []string{"/私存入库"}, "/BON_115网盘")

	var n int64
	p := newPushConsumer(nil, 20*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
	var lmu sync.Mutex
	var logged []string
	p.log = func(format string, args ...any) {
		lmu.Lock()
		logged = append(logged, fmt.Sprintf(format, args...))
		lmu.Unlock()
	}

	p.onEvent(PushEvent{Type: 4, Path: ""})
	time.Sleep(10 * p.debounce)
	if got := atomic.LoadInt64(&n); got != 0 {
		t.Fatalf("无路径事件不应触发扫描，实际 %d 次", got)
	}
	if ig := pushSnapshot().IgnoredEvents; ig < 1 {
		t.Fatalf("无路径事件应计入 ignoredEvents，实际 %d", ig)
	}
	lmu.Lock()
	joined := strings.Join(logged, "\n")
	lmu.Unlock()
	if !strings.Contains(joined, "变更事件未携带路径") {
		t.Fatalf("应节流记录「无路径」跳过，实际日志：%q", joined)
	}
}

// ==================== F2：扫描冷却（最小间隔 + 末尾触发）====================

func TestEventScanCooldown_DefersAndTrails(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)
	withCleanScope(t, []string{"/私存入库"}, "/BON_115网盘")

	var n int64
	p := newPushConsumer(nil, 10*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
	p.log = func(string, ...any) {}
	p.minInterval = 300 * time.Millisecond // 注入较小冷却值，避免测试过慢

	// 首个范围内事件：lastTriggerAt 为 0 → 立即触发一次。
	p.onEvent(PushEvent{Type: 4, Path: "/BON_115网盘/私存入库/a"})
	waitUntil(t, func() bool { return atomic.LoadInt64(&n) == 1 }, time.Second, "首个范围内事件应触发一次")

	// 冷却期内再来一个事件：防抖到期后应进入冷却 → 不立即扫描。
	p.onEvent(PushEvent{Type: 4, Path: "/BON_115网盘/私存入库/b"})
	time.Sleep(120 * time.Millisecond)
	if got := atomic.LoadInt64(&n); got != 1 {
		t.Fatalf("冷却期内不应再次扫描，实际 %d 次", got)
	}
	// 冷却结束：末尾触发（trailing）最终执行，保证最后一个变更不丢。
	waitUntil(t, func() bool { return atomic.LoadInt64(&n) >= 2 }, 900*time.Millisecond, "冷却结束应执行末尾触发")
}

func TestEventScanCooldown_ZeroIntervalAllowsMultiple(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)
	withCleanScope(t, []string{"/私存入库"}, "/BON_115网盘")

	var n int64
	p := newPushConsumer(nil, 20*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
	p.log = func(string, ...any) {}
	if p.minInterval != 0 {
		t.Fatalf("新建消费者的 minInterval 默认应为 0（关闭冷却），实际 %v", p.minInterval)
	}
	p.onEvent(PushEvent{Type: 4, Path: "/BON_115网盘/私存入库/a"})
	waitUntil(t, func() bool { return atomic.LoadInt64(&n) == 1 }, time.Second, "首次触发")
	p.onEvent(PushEvent{Type: 4, Path: "/BON_115网盘/私存入库/b"})
	waitUntil(t, func() bool { return atomic.LoadInt64(&n) == 2 }, time.Second, "minInterval=0 时应可再次触发（旧行为）")
}

// ==================== F2：配置默认值 / 规范化 ====================

func TestEventScanMinIntervalConfig(t *testing.T) {
	if got := defaultConfig().EventScanMinIntervalMinutes; got != 5 {
		t.Fatalf("默认事件扫描最小间隔应为 5，实际 %d", got)
	}
	if got := normalizeConfig(Config{EventScanMinIntervalMinutes: 0}).EventScanMinIntervalMinutes; got != 0 {
		t.Fatalf("0 应保留（关闭冷却），实际 %d", got)
	}
	if got := normalizeConfig(Config{EventScanMinIntervalMinutes: 30}).EventScanMinIntervalMinutes; got != 30 {
		t.Fatalf("合法值应保留，实际 %d", got)
	}
	if got := normalizeConfig(Config{EventScanMinIntervalMinutes: 99999}).EventScanMinIntervalMinutes; got != 5 {
		t.Fatalf("超范围值应回退默认 5，实际 %d", got)
	}
}
