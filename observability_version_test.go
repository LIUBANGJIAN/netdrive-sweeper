package main

// observability_version_test.go —— 本轮「版本号展示」与「离线任务监控可观测性」回归测试。
//
// 背景（用户报障）：
//  1. 页面左上角只有「NetDrive Sweeper」，无法区分部署的是哪个版本 → 需展示版本号；
//  2. 「离线任务监控没有生效，没有日志，没有清理垃圾文件」——根因是部署陈旧（未重建/未迁移配置），
//     但当时界面与日志对该功能「是否在跑」毫无可观测性，用户无从自证，故本次补齐：
//     /api/state、/api/push 输出 offlineMonitor 运行态；监控循环在 启用/关闭/间隔变化/下载中目录变化
//     时均写日志；页面对应展示状态条。
//
// 本文件覆盖：
//   - versionLabel 非空、以 v 开头、包含 appVersion；intervalText 渲染；
//   - GET / 渲染版本角标（class="ver"，紧随标题）；
//   - GET /api/state 与 /api/push 均含 offlineMonitor 字段及其子字段；
//   - offlineMonSnapshot 为深拷贝（外部改动不污染内部状态）；
//   - setOfflineMonState / markOfflineMonCheck / markOfflineMonTrigger 状态机；
//   - offlineMonDisabledReason 对「间隔=0」与「无清理目录」给出可读原因；
//   - offlineMonitor 循环在「无清理目录」时确实写出可读日志（直接回答「为什么没有日志」）。

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// ---- 版本号 ----

func TestVersionLabel_NonEmptyAndPrefixed(t *testing.T) {
	if appVersion == "" {
		t.Fatal("appVersion 不应为空（构建时可用 -ldflags -X main.appVersion 覆盖）")
	}
	if versionLabel == "" {
		t.Fatal("versionLabel 不应为空")
	}
	if !strings.HasPrefix(versionLabel, "v"+appVersion) {
		t.Fatalf("versionLabel=%q 应以 v%s 开头", versionLabel, appVersion)
	}
}

func TestIntervalText(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "关闭"},
		{-3, "关闭"},
		{1, "每 1 分钟"},
		{15, "每 15 分钟"},
	}
	for _, tc := range cases {
		if got := intervalText(tc.in); got != tc.want {
			t.Errorf("intervalText(%d)=%q, 期望 %q", tc.in, got, tc.want)
		}
	}
}

// TestHandleIndex_RendersVersionBadge 版本号必须紧跟在标题文本之后（同一 <h1> 内的角标），
// 用户才能在左上角一眼看到版本。
func TestHandleIndex_RendersVersionBadge(t *testing.T) {
	rec := httptest.NewRecorder()
	handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("GET / code=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="ver"`) {
		t.Fatalf("页面缺少版本角标样式类 class=\"ver\"")
	}
	want := "NetDrive Sweeper<span class=\"ver\">" + versionLabel + "</span>"
	if !strings.Contains(body, want) {
		t.Fatalf("页面未在标题旁渲染版本号；期望包含 %q", want)
	}
}

// ---- 离线监控可观测性 ----

func saveOffMonState(t *testing.T) func() {
	t.Helper()
	offMonMu.Lock()
	old := offMonStat
	if offMonStat.Watching != nil {
		old.Watching = append([]string(nil), offMonStat.Watching...)
	}
	offMonMu.Unlock()
	return func() {
		offMonMu.Lock()
		offMonStat = old
		offMonMu.Unlock()
	}
}

func TestAPI_StateAndPush_IncludeOfflineMonitor(t *testing.T) {
	defer withTempPaths(t)()
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	for _, name := range []string{"/api/state", "/api/push"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", name, nil)
		if name == "/api/state" {
			handleState(rec, req)
		} else {
			handlePush(rec, req)
		}
		if rec.Code != 200 {
			t.Fatalf("%s code=%d body=%s", name, rec.Code, rec.Body.String())
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s 不是合法 JSON: %v", name, err)
		}
		raw, ok := payload["offlineMonitor"]
		if !ok {
			t.Fatalf("%s 缺少 offlineMonitor 字段（离线监控必须可观测）", name)
		}
		var om OfflineMonitorRuntime
		if err := json.Unmarshal(raw, &om); err != nil {
			t.Fatalf("%s.offlineMonitor 解码失败: %v", name, err)
		}
		// 字段口径（供前端渲染）：enabled / intervalMinutes / triggers 必须出现，
		// 否则前端无法区分「未生效」与「在跑但暂无任务完成」。
		for _, k := range []string{`"enabled"`, `"intervalMinutes"`, `"triggers"`} {
			if !strings.Contains(string(raw), k) {
				t.Fatalf("%s.offlineMonitor 缺少子字段 %s，实际 %s", name, k, string(raw))
			}
		}
	}
}

func TestOfflineMonSnapshot_IsDeepCopy(t *testing.T) {
	defer saveOffMonState(t)()
	markOfflineMonCheck([]string{"/电影", "/剧集"})

	snap := offlineMonSnapshot()
	if len(snap.Watching) != 2 {
		t.Fatalf("快照 watching=%v, 期望 2 项", snap.Watching)
	}
	snap.Watching[0] = "被污染"
	snap.Watching = append(snap.Watching, "新增")

	again := offlineMonSnapshot()
	if len(again.Watching) != 2 || again.Watching[0] != "/电影" {
		t.Fatalf("快照未做深拷贝，内部状态被污染: %v", again.Watching)
	}
}

func TestSetOfflineMonState_TracksEnabledAndClearsWatching(t *testing.T) {
	defer saveOffMonState(t)()

	setOfflineMonState(true, 1, "运行中")
	markOfflineMonCheck([]string{"/电影"})
	s1 := offlineMonSnapshot()
	if !s1.Enabled || s1.IntervalMinutes != 1 || s1.Note != "运行中" {
		t.Fatalf("启用态错误: %+v", s1)
	}
	if len(s1.Watching) != 1 {
		t.Fatalf("启用时 watching=%v, 期望 1 项", s1.Watching)
	}
	if s1.Since == "" {
		t.Fatal("状态变化应记录 Since")
	}

	// 关闭时必须清空陈旧「下载中」痕迹，避免误导用户。
	setOfflineMonState(false, 0, "已关闭")
	s2 := offlineMonSnapshot()
	if s2.Enabled || s2.IntervalMinutes != 0 {
		t.Fatalf("关闭态错误: %+v", s2)
	}
	if len(s2.Watching) != 0 {
		t.Fatalf("关闭后应清空 watching，实际 %v", s2.Watching)
	}
}

func TestMarkOfflineMonTrigger_Accumulates(t *testing.T) {
	defer saveOffMonState(t)()
	offMonMu.Lock()
	offMonStat.Triggers = 0
	offMonMu.Unlock()

	markOfflineMonTrigger()
	markOfflineMonTrigger()
	s := offlineMonSnapshot()
	if s.Triggers != 2 {
		t.Fatalf("triggers=%d, 期望 2", s.Triggers)
	}
	if s.LastTriggerAt == "" {
		t.Fatal("触发后应记录 LastTriggerAt")
	}
}

func TestOfflineMonDisabledReason(t *testing.T) {
	cases := []struct {
		minutes, tasks int
		wantSub        string
	}{
		{0, 3, "已关闭"},
		{1, 0, "无清理目录"},
		{1, 2, "未启用"},
	}
	for _, tc := range cases {
		got := offlineMonDisabledReason(tc.minutes, tc.tasks)
		if !strings.Contains(got, tc.wantSub) {
			t.Errorf("offlineMonDisabledReason(%d,%d)=%q, 期望含 %q", tc.minutes, tc.tasks, got, tc.wantSub)
		}
	}
}

// TestOfflineMonitor_LogsWhenNoTasks 直接回答用户「离线监控为什么没有日志」：
// 无清理目录时，循环必须写出可读日志（而非静默），并能在 ctx 取消后热退出（不泄漏 goroutine）。
func TestOfflineMonitor_LogsWhenNoTasks(t *testing.T) {
	defer withTempPaths(t)()

	c := defaultConfig()
	c.Tasks = nil // 空目录 = 不监控
	stateMu.Lock()
	cfg = c
	stateMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		offlineMonitor(ctx)
	}()

	// 让循环至少完成一轮「未运行」判定并写日志。
	time.Sleep(120 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("offlineMonitor 未在 ctx 取消后退出（goroutine 泄漏）")
	}

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取日志失败（说明监控完全没写日志）: %v", err)
	}
	if !strings.Contains(string(b), "离线任务监控：未运行") {
		t.Fatalf("无清理目录时应写出「离线任务监控：未运行」日志，实际日志:\n%s", string(b))
	}
	// 未运行时状态也必须暴露给页面（enabled=false 且 note 可读）。
	s := offlineMonSnapshot()
	if s.Enabled {
		t.Fatalf("无清理目录时不应处于启用态: %+v", s)
	}
	if s.Note == "" || s.Note == "未启动" {
		t.Fatalf("未运行时应给出可读原因，实际 note=%q", s.Note)
	}
}
