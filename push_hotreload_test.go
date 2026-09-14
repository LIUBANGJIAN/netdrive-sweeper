package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestMigrateConfig_LegacyCooldown 验证 v0→v1 迁移：旧默认 6h 冷却改为 0（立即清理），
// 用户自定义值与已迁移版本一概不改。
func TestMigrateConfig_LegacyCooldown(t *testing.T) {
	got := migrateConfig(Config{ConfigVersion: 0, FileCooldownHours: 6})
	if got.FileCooldownHours != 0 {
		t.Fatalf("旧默认 6h 应迁移为 0，实际 %d", got.FileCooldownHours)
	}
	if got.ConfigVersion != currentConfigVersion {
		t.Fatalf("迁移后版本应=%d，实际 %d", currentConfigVersion, got.ConfigVersion)
	}
	if c := migrateConfig(Config{ConfigVersion: 0, FileCooldownHours: 3}); c.FileCooldownHours != 3 {
		t.Fatalf("用户自定义冷却不应被迁移，实际 %d", c.FileCooldownHours)
	}
	if c := migrateConfig(Config{ConfigVersion: currentConfigVersion, FileCooldownHours: 6}); c.FileCooldownHours != 6 {
		t.Fatalf("已迁移版本不应再次改写，实际 %d", c.FileCooldownHours)
	}
}

// TestPushConfigSignature_TracksSave 验证「保存配置」会让订阅指纹变化 → 触发热重启。
func TestPushConfigSignature_TracksSave(t *testing.T) {
	c := Config{Address: "127.0.0.1:19798", Token: "tok", EnablePush: true, PushDebounceSeconds: 5}
	before := pushConfigSignature(c)
	wakePushSupervisor() // 模拟一次「保存配置」
	after := pushConfigSignature(c)
	if before == after {
		t.Fatalf("保存后指纹必须变化以触发订阅热重启：%q == %q", before, after)
	}
}

// TestPushDisabledReason 验证「未启动原因」文案分类正确。
func TestPushDisabledReason(t *testing.T) {
	if got := pushDisabledReason(Config{EnablePush: false}); !strings.Contains(got, "已关闭") {
		t.Fatalf("关闭状态原因=%q，期望包含「已关闭」", got)
	}
	if got := pushDisabledReason(Config{EnablePush: true}); !strings.Contains(got, "地址或 Token") {
		t.Fatalf("缺配置原因=%q，期望包含「地址或 Token」", got)
	}
}

// TestHandlePush_ReturnsSnapshot 验证 /api/push 返回 ok + push 字段。
func TestHandlePush_ReturnsSnapshot(t *testing.T) {
	rec := httptest.NewRecorder()
	handlePush(rec, httptest.NewRequest("GET", "/api/push", nil))
	if !strings.Contains(rec.Body.String(), `"push"`) {
		t.Fatalf("body=%s，期望包含 push 字段", rec.Body.String())
	}
}

// TestSetPushStateAndSnapshot 验证状态写入与读取一致。
func TestSetPushStateAndSnapshot(t *testing.T) {
	defer setPushState("off", "未启用")
	setPushState("running", "测试中")
	snap := pushSnapshot()
	if snap.State != "running" || snap.Detail != "测试中" || snap.Since == "" {
		t.Fatalf("snapshot=%+v，期望 state=running detail=测试中 且 Since 非空", snap)
	}
}

// TestBumpPushEvent 验证事件计数自增。
func TestBumpPushEvent(t *testing.T) {
	pushMu.Lock()
	old := pushStat.Events
	pushMu.Unlock()
	bumpPushEvent()
	pushMu.Lock()
	now := pushStat.Events
	pushMu.Unlock()
	if now != old+1 {
		t.Fatalf("事件计数应从 %d → %d，实际 %d", old, old+1, now)
	}
}

// TestPushTypeName 验证消息类型命名覆盖官方 9 种类型与未知值。
func TestPushTypeName(t *testing.T) {
	if pushTypeName(4) != "FILE_SYSTEM_CHANGE" {
		t.Fatalf("类型 4 应为 FILE_SYSTEM_CHANGE，实际 %s", pushTypeName(4))
	}
	if pushTypeName(99) != "UNKNOWN" {
		t.Fatalf("未知类型应为 UNKNOWN，实际 %s", pushTypeName(99))
	}
}

// TestPushSupervisor_DisabledConfigStops 验证未启用时监督器不启动订阅，
// 并把状态置为 config_missing（这是「改配置无需重启容器」的状态机入口）。
func TestPushSupervisor_DisabledConfigStops(t *testing.T) {
	stateMu.Lock()
	old := cfg
	cfg.EnablePush = false
	cfg.Address = ""
	cfg.Token = ""
	stateMu.Unlock()
	defer func() {
		stateMu.Lock()
		cfg = old
		stateMu.Unlock()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { pushSupervisor(ctx); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pushSnapshot().State == "config_missing" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("监督器未在 ctx 取消后退出")
	}
	if got := pushSnapshot().State; got != "config_missing" {
		t.Fatalf("未启用时状态应为 config_missing，实际 %s", got)
	}
}

// TestPushConsumer_NoteTypeDedup 验证消息类型首次到达只记一次（避免日志刷屏）。
func TestPushConsumer_NoteTypeDedup(t *testing.T) {
	var logs []string
	p := newPushConsumer(nil, time.Second, nil)
	p.log = func(format string, args ...any) { logs = append(logs, format) }
	p.noteType(2)
	p.noteType(2)
	p.noteType(4)
	if len(logs) != 2 {
		t.Fatalf("去重后应只记 2 条（类型 2、4 各一次），实际 %d 条：%v", len(logs), logs)
	}
}
