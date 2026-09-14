package main

// push_gen_test.go —— 「世代化订阅状态 + 存活看门狗 + panic 兜底 + 心跳可观测」回归测试。
// 覆盖 team-lead 规格要求：
//	(a)/(d) setPushStateIfGen 对过期世代丢弃、当前世代生效；旧世代的 off 不得覆盖新世代 running；
//	(b) 看门狗：pushDone 已关闭但 pushStop != nil 时判定“假运行”并触发重建（含纯函数单测 + 监督器集成测试）；
//	(c) runPushConsumer 的 panic 兜底会记日志、不会 panic 逃逸、且退出会对账状态；
//	前端 renderPush 暴露世代/已建立/最近推送/重连/最近失败 与 30 分钟存活提示。
//
// 所有会经 appendLog 落盘的用例一律重定向到 t.TempDir()（withTempPaths），避免污染 data/clean.log。

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// withPushGlobals 保存/恢复推送相关的全局状态，避免用例间互相污染。
// 起手把在跑标记、世代、状态复位，并用 t.Cleanup 恢复原值。
func withPushGlobals(t *testing.T) {
	t.Helper()
	pushMu.Lock()
	oldStat := pushStat
	oldStop := pushStop
	oldDone := pushDone
	oldSig := pushSig
	pushStop, pushDone, pushSig = nil, nil, ""
	pushStat = PushRuntime{State: "off", Detail: "未启用"}
	pushMu.Unlock()
	oldGen := pushGen.Load()
	pushGen.Store(0)
	oldLaunch := pushLaunch
	oldConnect := pushConnect
	t.Cleanup(func() {
		pushLaunch = oldLaunch
		pushConnect = oldConnect
		pushMu.Lock()
		if pushStop != nil {
			pushStop()
		}
		pushStop, pushDone, pushSig = oldStop, oldDone, oldSig
		pushStat = oldStat
		pushMu.Unlock()
		pushGen.Store(oldGen)
	})
}

// waitUntil 轮询等待条件成立；超时则判定失败。
func waitUntil(t *testing.T, cond func() bool, d time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("%s（等待 %v 超时）", msg, d)
	}
}

// ==================== (a)/(d) 世代守卫 ====================

// 当前世代写入生效；世代前进后，过期世代的写入必须被丢弃（防止旧订阅收尾覆盖新订阅状态）。
func TestGenGuard_StaleWritesDropped(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	pushGen.Store(7)
	if !setPushStateIfGen("running", "g7 运行", 7) {
		t.Fatal("当前世代写入应返回 true")
	}
	if got := pushSnapshot(); got.State != "running" || got.Gen != 7 {
		t.Fatalf("当前世代写入未生效：state=%q gen=%d", got.State, got.Gen)
	}

	// 热重启：世代前进到 8。旧世代（7）的收尾写入必须被丢弃。
	pushGen.Store(8)
	if setPushStateIfGen("off", "旧世代退出", 7) {
		t.Fatal("过期世代（7）的写入应返回 false 被丢弃")
	}
	if got := pushSnapshot(); got.State != "running" || got.Gen != 7 {
		t.Fatalf("旧世代的 off 覆盖了新世代状态：state=%q", got.State)
	}

	// 新世代仍可写。
	if !setPushStateIfGen("off", "g8 退出", 8) {
		t.Fatal("新世代写入应返回 true")
	}
	if got := pushSnapshot(); got.State != "off" || got.Gen != 8 {
		t.Fatalf("新世代写入未生效：state=%q gen=%d", got.State, got.Gen)
	}
}

// 进入 running 时应记录 SubscribedAt（供前端「已建立 Xm」）。
func TestGenGuard_RunningSetsSubscribedAt(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	pushGen.Store(1)
	if !setPushStateIfGen("running", "运行", 1) {
		t.Fatal("running 写入应成功")
	}
	if sn := pushSnapshot(); sn.SubscribedAt == "" {
		t.Fatal("进入 running 应记录 SubscribedAt")
	}
	// 世代守卫的重连计数写入：当前世代生效、过期世代丢弃。
	markPushReconnect(1, 3)
	if pushSnapshot().Reconnects != 3 {
		t.Fatalf("当前世代重连计数应写入，实际 %d", pushSnapshot().Reconnects)
	}
	pushGen.Store(2)
	markPushReconnect(1, 99)
	if pushSnapshot().Reconnects != 3 {
		t.Fatalf("过期世代的重连计数应被丢弃，实际 %d", pushSnapshot().Reconnects)
	}
	markPushLastError(1, "旧世代错误")
	if pushSnapshot().LastError != "" {
		t.Fatalf("过期世代的错误应被丢弃，实际 %q", pushSnapshot().LastError)
	}
	markPushLastError(2, "当前世代错误")
	if pushSnapshot().LastError != "当前世代错误" {
		t.Fatalf("当前世代的错误应写入，实际 %q", pushSnapshot().LastError)
	}
}

// ==================== (b) 看门狗 ====================

// 纯函数层：pushDone 已关闭但 pushStop != nil ⇒ 判定“假运行”，清态并报告需重建。
func TestWatchdogLocked_DetectsDead(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	// 布置：自称在跑 + done 已关闭。
	done := make(chan struct{})
	closed := false
	pushMu.Lock()
	pushStop = func() { closed = true }
	pushSig = "sig"
	pushDone = done
	pushStat.Gen = 5
	pushMu.Unlock()
	close(done)

	pushMu.Lock()
	deadGen, dead := pushWatchdogLocked()
	pushMu.Unlock()
	if !dead {
		t.Fatal("done 已关闭且 pushStop!=nil 时应判定为死")
	}
	if deadGen != 5 {
		t.Fatalf("应报告已死世代 5，实际 %d", deadGen)
	}
	if !closed {
		t.Fatal("判定死亡时应调用旧的 cancel（pushStop）")
	}
	if pushStop != nil || pushDone != nil || pushSig != "" {
		t.Fatalf("判定死亡后应清空在跑标记：stop=%v done=%v sig=%q", pushStop != nil, pushDone != nil, pushSig)
	}

	// 反例：done 未关闭 ⇒ 不判定死亡。
	d2 := make(chan struct{})
	pushMu.Lock()
	pushStop = func() {}
	pushDone = d2
	if _, dead := pushWatchdogLocked(); dead {
		t.Fatal("done 未关闭时不应判定死亡")
	}
	pushMu.Unlock()
}

// 集成层：真跑监督器，消费者“意外退出”（done 关闭）后必须被自动重建，
// 且日志出现看门狗专属行「事件驱动订阅意外退出」。
func TestWatchdog_RebuildsDeadConsumer(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	// 注入假消费者：记录 done 通道，不主动关闭（模拟“存活”），由测试决定何时模拟死亡。
	var mu sync.Mutex
	var dones []chan struct{}
	pushLaunch = func(_ context.Context, _ Config, _ int64, done chan struct{}) {
		mu.Lock()
		dones = append(dones, done)
		mu.Unlock()
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(dones) }

	// 配置为「应启动订阅」。
	stateMu.Lock()
	oldCfg := cfg
	cfg = Config{EnablePush: true, Address: "127.0.0.1:1", Token: "x", PushDebounceSeconds: 1, Tasks: []string{"/a"}}
	stateMu.Unlock()
	defer func() { stateMu.Lock(); cfg = oldCfg; stateMu.Unlock() }()

	ctx, cancel := context.WithCancel(context.Background())
	supDone := make(chan struct{})
	go func() { pushSupervisor(ctx); close(supDone) }()

	waitUntil(t, func() bool { return count() >= 1 }, 3*time.Second, "监督器未启动消费者")

	// 模拟消费者意外退出：关闭 done，但 pushStop 仍非 nil（自称在跑）→ 触发看门狗。
	mu.Lock()
	d1 := dones[0]
	mu.Unlock()
	close(d1)
	wakePushSupervisor() // 立即巡检（否则需等 20s 兜底）

	waitUntil(t, func() bool { return count() >= 2 }, 3*time.Second, "看门狗未重建意外退出的订阅")

	cancel()
	select {
	case <-supDone:
	case <-time.After(3 * time.Second):
		t.Fatal("监督器未在 ctx 取消后退出")
	}

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取日志失败: %v", err)
	}
	if !strings.Contains(string(b), "事件驱动订阅意外退出") {
		t.Fatalf("看门狗未留下诊断日志：\n%s", string(b))
	}
}

// ==================== (c) panic 兜底 ====================

// runPushConsumer 内发生 panic 时：不逃逸、记日志、done 关闭、状态对账为「已退出」。
func TestRunPushConsumerRecoversPanic(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	pushConnect = func(context.Context, Config) (*CD2Client, *TokenInfo, error) {
		panic("boom-测试专用")
	}
	pushGen.Store(1)

	done := make(chan struct{})
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic 逃逸出 runPushConsumer：%v", r)
			}
		}()
		runPushConsumer(context.Background(),
			Config{EnablePush: true, Address: "127.0.0.1:1", Token: "x", PushDebounceSeconds: 1},
			1, done)
	}()

	// D2 死亡信号：done 必须已关闭。
	select {
	case <-done:
	default:
		t.Fatal("runPushConsumer 返回后 done 未关闭（看门狗将无法感知死亡）")
	}
	// D1 退出对账：状态必须体现“已退出”，且带 panic 原因。
	if sn := pushSnapshot(); sn.State != "off" || !strings.Contains(sn.Detail, "panic") {
		t.Fatalf("退出未对账：state=%q detail=%q", sn.State, sn.Detail)
	}
	// D4 日志留痕。
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取日志失败: %v", err)
	}
	if !strings.Contains(string(b), "panic") {
		t.Fatalf("日志未记录 panic：\n%s", string(b))
	}
}

// ==================== 前端：存活可观测标记 + 内联 JS 安全 ====================

func TestWebStaticMarkers_PushLiveness(t *testing.T) {
	must := []string{
		"function parseTS(",
		"function agoText(",
		"function durText(",
		"'世代 '+lastPush.gen",
		"'已建立 '+durText(lastPush.subscribedAt)",
		"'最近收到推送 '+agoText(lastPush.lastMessageAt)",
		"'重连 '+lastPush.reconnects",
		"订阅存活但已超过 30 分钟未收到任何推送",
		"最近一次订阅失败：",
		"lastPush.lastError",
	}
	for _, m := range must {
		if !strings.Contains(pageHTML, m) {
			t.Fatalf("pageHTML 缺少存活可观测标记 %q", m)
		}
	}
	// 内联 JS 区间不得出现 {{（Go 模板）或游离反引号（会破坏页面原始字符串）。
	jsStart := strings.Index(pageHTML, "<script>")
	jsEnd := strings.LastIndex(pageHTML, "</script>")
	if jsStart < 0 || jsEnd < jsStart {
		t.Fatal("未找到内联 <script> 区间")
	}
	if strings.Contains(pageHTML[jsStart:jsEnd], "{{") {
		t.Fatal("内联 JS 出现 {{，会破坏 node --check")
	}
}
