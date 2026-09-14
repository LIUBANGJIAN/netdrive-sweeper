package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ---------- B1 回归：防抖最大延迟上限，消除「持续事件流饿死触发」 ----------

// TestPushConsumer_MaxDelayPreventsStarvation 复现「有时行有时不行」的根因：
// 纯防抖下，只要事件持续到来，定时器就会被不断顺延、trigger 永远不会触发。
// 加入 maxDelay 上限后，即便事件流不断，也必须在 maxDelay 内至少触发一次。
func TestPushConsumer_MaxDelayPreventsStarvation(t *testing.T) {
	const debounce = 50 * time.Millisecond
	fired := make(chan time.Time, 8)
	p := newPushConsumer(nil, debounce, func() { fired <- time.Now() })
	p.maxDelay = 200 * time.Millisecond // 显式设定上限（默认是 6×debounce），测试更聚焦
	p.log = func(string, ...any) {}

	stop := make(chan struct{})
	start := time.Now()
	go func() {
		tk := time.NewTicker(10 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				p.handle(4) // 持续不断的 FILE_SYSTEM_CHANGE 事件流
			}
		}
	}()

	select {
	case got := <-fired:
		close(stop)
		if waited := got.Sub(start); waited > p.maxDelay+150*time.Millisecond {
			t.Fatalf("持续事件流下 trigger 应在 maxDelay(%v) 内触发，实际等待 %v（防抖饥饿回归）", p.maxDelay, waited)
		}
	case <-time.After(2 * time.Second):
		close(stop)
		t.Fatal("持续事件流下 trigger 从未触发：防抖饥饿回归")
	}
}

// TestPushConsumer_MaxDelayFieldDefault 确认 maxDelay 默认取 6×debounce（5s→30s）。
func TestPushConsumer_MaxDelayFieldDefault(t *testing.T) {
	p := newPushConsumer(nil, 5*time.Second, nil)
	if p.maxDelay != 30*time.Second {
		t.Fatalf("maxDelay 默认应为 6×debounce=30s，实际 %v", p.maxDelay)
	}
	p2 := newPushConsumer(nil, 0, nil) // debounce<=0 回退 5s
	if p2.debounce != 5*time.Second || p2.maxDelay != 30*time.Second {
		t.Fatalf("debounce<=0 应回退 5s 且 maxDelay=30s，实际 debounce=%v maxDelay=%v", p2.debounce, p2.maxDelay)
	}
}

// ---------- B1 回归：rearm 后仍会再次触发（扫描互斥延后重试用） ----------

// TestPushConsumer_RearmRetriggers 验证 rearm() 会把未处理的这一轮重新排期，
// 在 debounce 后再次触发一次 trigger。
func TestPushConsumer_RearmRetriggers(t *testing.T) {
	const debounce = 40 * time.Millisecond
	fired := make(chan struct{}, 4)
	p := newPushConsumer(nil, debounce, func() { fired <- struct{}{} })
	p.log = func(string, ...any) {}

	p.handle(4)
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("首次防抖未触发")
	}

	p.rearm() // 模拟「扫描互斥，本轮未处理」→ 延后重试
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("rearm 后未再次触发")
	}
}

// ---------- A2 回归：缺 push 权限时也要点亮连接状态与权限徽章 ----------

// TestMarkPushDeniedConnected_WritesStatus 验证「缺少 allow_push_message」分支会写入
// 「已连接」状态与 Token，从而让连接状态与权限徽章无需手点「测试连接」即可点亮。
func TestMarkPushDeniedConnected_WritesStatus(t *testing.T) {
	stateMu.Lock()
	oldStatus := statusInfo
	stateMu.Unlock()
	defer func() {
		stateMu.Lock()
		statusInfo = oldStatus
		stateMu.Unlock()
	}()

	tok := &TokenInfo{RootDir: "/我的网盘", AllowList: true}
	markPushDeniedConnected(tok)

	stateMu.Lock()
	msg := statusInfo.LastMessage
	gotTok := statusInfo.Token
	stateMu.Unlock()

	if !strings.Contains(msg, "已连接") {
		t.Fatalf("缺少 push 权限时应写入「已连接」状态以点亮徽章，实际 lastMessage=%q", msg)
	}
	if !strings.Contains(msg, "/我的网盘") {
		t.Fatalf("状态应包含 Token 根目录，实际 %q", msg)
	}
	if gotTok == nil || !gotTok.AllowList {
		t.Fatalf("状态应附带 Token（含权限），实际 %+v", gotTok)
	}
}

// TestMarkPushDeniedConnected_NilSafe 确认 nil token 不会 panic，也不会改写状态。
func TestMarkPushDeniedConnected_NilSafe(t *testing.T) {
	stateMu.Lock()
	old := statusInfo
	stateMu.Unlock()
	markPushDeniedConnected(nil)
	stateMu.Lock()
	now := statusInfo
	stateMu.Unlock()
	if now.LastMessage != old.LastMessage {
		t.Fatalf("nil token 不应改写状态：%q → %q", old.LastMessage, now.LastMessage)
	}
}

// ---------- A1 回归：statusMonitor 常驻但可被 ctx 取消，不泄漏 goroutine ----------

func TestStatusMonitor_ReturnsOnContextCancel(t *testing.T) {
	stateMu.Lock()
	old := cfg
	cfg = Config{} // 未配置地址/Token → 走 15s 等待分支
	stateMu.Unlock()
	defer func() {
		stateMu.Lock()
		cfg = old
		stateMu.Unlock()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { statusMonitor(ctx); close(done) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("statusMonitor 未在 ctx 取消后退出（goroutine 泄漏）")
	}
	stateMu.Lock()
	msg := statusInfo.LastMessage
	stateMu.Unlock()
	if msg != "未配置 CD2 地址或 Token" {
		t.Fatalf("未配置时应写入「未配置 CD2 地址或 Token」，实际 %q", msg)
	}
}

// ---------- C 回归：页面不再有秒级计时 / 全屏遮罩，且具备运行期实时滚动 ----------

func TestWebStaticMarkers_NoRunElapsedTimer(t *testing.T) {
	// 反回归：旧的秒级计时与 #busy 全屏遮罩必须彻底移除。
	for _, bad := range []string{"已耗时", "setInterval(tickRun", "function tickRun(", "el('busyText')", "el('busy').style.display"} {
		if strings.Contains(pageHTML, bad) {
			t.Fatalf("pageHTML 不应再包含旧计时/遮罩逻辑 %q", bad)
		}
	}
	// 正向：运行期日志实时滚动必须在位。
	for _, want := range []string{
		`function startRunPolling(){`,
		`runPollTimer=setInterval(function(){loadLogs()`,
		`el('runState').textContent='运行中…'`,
		`activeTab==='logs'&&logState.follow&&!runPollTimer`,
	} {
		if !strings.Contains(pageHTML, want) {
			t.Fatalf("pageHTML 缺少运行期日志滚动标记 %q", want)
		}
	}
}
