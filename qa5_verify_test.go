package main

// qa5_verify_test.go —— 第二层独立验证（针对 9396c58：世代化订阅状态 + 存活看门狗 + panic 兜底 + 心跳可观测）。
//
// 与实现者的 push_gen_test.go 相互独立，重点从「对抗性 / 时序 / 边界」角度证明改动可用：
//	(a) 世代守卫：过期写入必须「不改动 pushStat 的实际内容」（而非只看返回值）。
//	(b) 看门狗顺序：断言 pushSupervisor 内 pushWatchdogLocked 早于重建分支、且在临界区内；
//	    并用「直接向 pushWake 投递」的方式隔离出「配置未变、仅消费者死亡」的巡检（避免与
//	    wakePushSupervisor 的 pushRev 变更触发的热重启混淆），确定性验证「死亡→清态→重建」。
//	(c) panic 兜底不逃逸；runtime.Goexit 不得被当 panic。
//	(d) done 关闭是 runPushConsumer 的第一条语句（无「panic 早于 defer 注册」的窗口）。
//	(e) 热重启：旧世代的 off 不得覆盖新世代的 connecting/running。
//	(g) 风控红线：生产代码无任何周期性 ticker，看门狗只做控制面判断。
//
// 所有会经 appendLog 落盘的用例一律 withTempPaths 重定向，绝不污染 data/clean.log。

import (
	"context"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ==================== (a) 世代守卫：过期写入不得改动状态内容 ====================

func TestQA5_GenGuardStaleWriteNoMutation(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	pushGen.Store(2)
	if !setPushStateIfGen("running", "gen2-running", 2) {
		t.Fatal("当前世代（2）写入应生效")
	}
	before := pushSnapshot()
	if before.State != "running" || before.Gen != 2 || before.Detail != "gen2-running" {
		t.Fatalf("当前世代写入未生效：%+v", before)
	}

	// 过期世代（1）写入：返回值必须为 false，且 pushStat 的每个字段都不许变。
	if setPushStateIfGen("off", "gen1-stale", 1) {
		t.Fatal("过期世代写入应返回 false 被丢弃")
	}
	after := pushSnapshot()
	if after.State != before.State || after.Detail != before.Detail ||
		after.Gen != before.Gen || after.Since != before.Since {
		t.Fatalf("过期世代写入改动了 pushStat 内容：\n before=%+v\n after =%+v", before, after)
	}

	// 当前世代（2）再次写入必须生效。
	if !setPushStateIfGen("error", "gen2-error", 2) {
		t.Fatal("当前世代写入应生效")
	}
	got := pushSnapshot()
	if got.State != "error" || got.Gen != 2 || got.Detail != "gen2-error" {
		t.Fatalf("当前世代写入未生效：%+v", got)
	}
}

// ==================== (b) 看门狗：顺序 + 死亡→重建 ====================

// (b) 结构：pushWatchdogLocked 必须早于「重建分支」，且在 pushMu 临界区内调用；
// 且它在 running 取值之后仍显式把 running 置 false（补偿「先取值后判定」的时序）。
func TestQA5_SupervisorWatchdogOrdering(t *testing.T) {
	body := extractGoFunc(t, mustReadGoFile(t, "main.go"), "func pushSupervisor(ctx context.Context)")

	iRunning := strings.Index(body, "running := pushStop != nil")
	iWatchdog := strings.Index(body, "pushWatchdogLocked()")
	iForceFalse := strings.Index(body, "running = false")
	iRebuild := strings.Index(body, "if want && !running {")
	iUnlock := strings.Index(body, "pushMu.Unlock()")
	iLaunch := strings.Index(body, "pushLaunch(")

	if iRunning < 0 || iWatchdog < 0 || iForceFalse < 0 || iRebuild < 0 || iUnlock < 0 || iLaunch < 0 {
		t.Fatalf("pushSupervisor 结构锚点缺失：running=%d watchdog=%d forceFalse=%d rebuild=%d unlock=%d launch=%d",
			iRunning, iWatchdog, iForceFalse, iRebuild, iUnlock, iLaunch)
	}
	// 关键顺序：run 取值 → 看门狗 → 显式置 false → 重建分支。
	if !(iRunning < iWatchdog && iWatchdog < iForceFalse && iForceFalse < iRebuild) {
		t.Fatalf("看门狗顺序不满足 running<watchdog<forceFalse<rebuild：%d/%d/%d/%d",
			iRunning, iWatchdog, iForceFalse, iRebuild)
	}
	// 看门狗与 pushLaunch 均须在临界区内（早于 pushMu.Unlock）。
	if !(iWatchdog < iUnlock && iLaunch < iUnlock) {
		t.Fatalf("看门狗/启动未处于 pushMu 临界区内：watchdog=%d launch=%d unlock=%d", iWatchdog, iLaunch, iUnlock)
	}
}

// (b) 行为：隔离出「配置未变、仅消费者死亡」的一次巡检——直接向 pushWake 投递（不经过
// wakePushSupervisor，以免 pushRev 变更触发热重启分支，从而把看门狗的作用混淆掉）。
// 断言：旧消费者被取消（ctx.Err()!=nil）、旧 done 被替换为新的、并留下看门狗诊断日志。
func TestQA5_WatchdogRebuildsDeadConsumer(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	var mu sync.Mutex
	var dones []chan struct{}
	var ctxs []context.Context
	pushLaunch = func(ctx context.Context, _ Config, _ int64, done chan struct{}) {
		mu.Lock()
		dones = append(dones, done)
		ctxs = append(ctxs, ctx)
		mu.Unlock()
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(dones) }

	stateMu.Lock()
	oldCfg := cfg
	cfg = Config{EnablePush: true, Address: "127.0.0.1:1", Token: "x", PushDebounceSeconds: 1, Tasks: []string{"/a"}}
	stateMu.Unlock()
	defer func() { stateMu.Lock(); cfg = oldCfg; stateMu.Unlock() }()

	ctx, cancel := context.WithCancel(context.Background())
	supDone := make(chan struct{})
	go func() { pushSupervisor(ctx); close(supDone) }()

	waitUntil(t, func() bool { return count() >= 1 }, 3*time.Second, "监督器未启动消费者")

	mu.Lock()
	d1, c1 := dones[0], ctxs[0]
	mu.Unlock()
	if c1.Err() != nil {
		t.Fatal("首轮启动的消费者 ctx 不应已被取消")
	}

	// 模拟「消费者意外退出」：关闭 done，但 pushStop 仍非 nil（自称在跑）。
	close(d1)
	// 直接投递巡检令牌（不 bump pushRev ⇒ 配置指纹不变 ⇒ 排除热重启分支的干扰）。
	select {
	case pushWake <- struct{}{}:
	default:
	}

	waitUntil(t, func() bool { return count() >= 2 }, 3*time.Second, "看门狗未重建意外退出的订阅")

	mu.Lock()
	d2, c2 := dones[1], ctxs[1]
	mu.Unlock()
	if c1.Err() == nil {
		t.Fatal("判定死亡后应调用旧 cancel（旧消费者 ctx 应被取消）——看门狗未生效")
	}
	if d2 == d1 {
		t.Fatal("重建后 pushDone 应替换为新消费者的 done")
	}
	if c2 == c1 {
		t.Fatal("重建应使用全新的 ctx")
	}

	cancel()
	select {
	case <-supDone:
	case <-time.After(3 * time.Second):
		t.Fatal("监督器未随 ctx 取消退出")
	}

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取日志失败: %v", err)
	}
	if !strings.Contains(string(b), "事件驱动订阅意外退出") {
		t.Fatalf("看门狗未留下「意外退出 … 正在自动重建」日志：\n%s", string(b))
	}
}

// (b2) 同一次死亡不得被反复重建（新消费者存活时，后续巡检必须静默）。
func TestQA5_WatchdogNoDoubleRebuild(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	var mu sync.Mutex
	var dones []chan struct{}
	pushLaunch = func(_ context.Context, _ Config, _ int64, done chan struct{}) {
		mu.Lock()
		dones = append(dones, done)
		mu.Unlock()
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(dones) }

	stateMu.Lock()
	oldCfg := cfg
	cfg = Config{EnablePush: true, Address: "127.0.0.1:1", Token: "x", PushDebounceSeconds: 1, Tasks: []string{"/a"}}
	stateMu.Unlock()
	defer func() { stateMu.Lock(); cfg = oldCfg; stateMu.Unlock() }()

	ctx, cancel := context.WithCancel(context.Background())
	supDone := make(chan struct{})
	go func() { pushSupervisor(ctx); close(supDone) }()

	waitUntil(t, func() bool { return count() >= 1 }, 3*time.Second, "监督器未启动消费者")
	mu.Lock()
	d1 := dones[0]
	mu.Unlock()

	close(d1) // 一次死亡
	select {
	case pushWake <- struct{}{}:
	default:
	}
	waitUntil(t, func() bool { return count() >= 2 }, 3*time.Second, "首次死亡未被重建")

	// 新消费者（d2）未退出：再多次巡检都不得重复重建。
	for i := 0; i < 3; i++ {
		select {
		case pushWake <- struct{}{}:
		default:
		}
		time.Sleep(60 * time.Millisecond)
	}
	if got := count(); got != 2 {
		t.Fatalf("同一次死亡被重复重建：launched=%d，期望 2", got)
	}

	cancel()
	select {
	case <-supDone:
	case <-time.After(3 * time.Second):
		t.Fatal("监督器未随 ctx 取消退出")
	}
}

// (b1) 未启用配置时看门狗不得误判「死亡」/误报/误重建。
func TestQA5_WatchdogDisabledNoFalseDeath(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	var mu sync.Mutex
	launched := 0
	pushLaunch = func(_ context.Context, _ Config, _ int64, _ chan struct{}) {
		mu.Lock()
		launched++
		mu.Unlock()
	}

	stateMu.Lock()
	oldCfg := cfg
	cfg = Config{EnablePush: false, Address: "", Token: ""}
	stateMu.Unlock()
	defer func() { stateMu.Lock(); cfg = oldCfg; stateMu.Unlock() }()

	ctx, cancel := context.WithCancel(context.Background())
	supDone := make(chan struct{})
	go func() { pushSupervisor(ctx); close(supDone) }()

	// 干净态（无在跑消费者）：触发一次巡检。
	select {
	case pushWake <- struct{}{}:
	default:
	}
	time.Sleep(120 * time.Millisecond)

	pushMu.Lock()
	_, dead := pushWatchdogLocked()
	pushMu.Unlock()
	if dead {
		t.Fatal("无在跑消费者（pushStop==nil）时看门狗误判死亡")
	}
	if n := func() int { mu.Lock(); defer mu.Unlock(); return launched }(); n != 0 {
		t.Fatalf("未启用配置不应启动/重建消费者，实际 %d", n)
	}

	cancel()
	select {
	case <-supDone:
	case <-time.After(3 * time.Second):
		t.Fatal("监督器未随 ctx 取消退出")
	}

	if b, err := os.ReadFile(logPath); err == nil && strings.Contains(string(b), "意外退出") {
		t.Fatalf("未启用配置下不得出现「意外退出」误报：\n%s", string(b))
	}
}

// ==================== (c) panic 兜底 / Goexit ====================

func TestQA5_PanicRecoveredNoEscape(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	pushConnect = func(context.Context, Config) (*CD2Client, *TokenInfo, error) {
		panic("qa5-boom")
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
			Config{EnablePush: true, Address: "127.0.0.1:1", Token: "x", PushDebounceSeconds: 1}, 1, done)
	}()

	// D2 死亡信号：done 必须已关闭，否则看门狗永远测不到。
	select {
	case <-done:
	default:
		t.Fatal("panic 恢复后 done 未关闭（看门狗无法感知死亡）")
	}
	// D1 退出对账：非 ctx 取消的退出必须落到 off 且带 panic 原因。
	if sn := pushSnapshot(); sn.State != "off" || !strings.Contains(sn.Detail, "panic") {
		t.Fatalf("panic 退出未对账：state=%q detail=%q", sn.State, sn.Detail)
	}
	// D4 日志留痕：必须含「发生 panic」且带堆栈（debug.Stack 含 goroutine 字样）。
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取日志失败: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "事件驱动订阅发生 panic") {
		t.Fatalf("日志未记录 panic 事件：\n%s", s)
	}
	if !strings.Contains(s, "goroutine") {
		t.Fatalf("panic 日志未包含堆栈（应含 goroutine 字样）：\n%s", s)
	}
}

// runtime.Goexit 不是 panic：不得被当作 panic 记录；done 仍须关闭（看门狗可接手）。
func TestQA5_GoexitNotTreatedAsPanic(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	pushConnect = func(context.Context, Config) (*CD2Client, *TokenInfo, error) {
		runtime.Goexit()
		return nil, nil, nil // 不可达；Goexit 终止当前 goroutine
	}
	pushGen.Store(1)

	done := make(chan struct{})
	go func() {
		runPushConsumer(context.Background(),
			Config{EnablePush: true, Address: "127.0.0.1:1", Token: "x", PushDebounceSeconds: 1}, 1, done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Goexit 终止 goroutine 后 done 未关闭")
	}
	b, err := os.ReadFile(logPath)
	if err == nil && strings.Contains(string(b), "事件驱动订阅发生 panic") {
		t.Fatalf("runtime.Goexit 被误判为 panic：\n%s", string(b))
	}
	// 注：Goexit 路径不产生任何日志（无 panic、无连接失败），logPath 可能不存在——属预期。
}

// ==================== (d) 假运行不可复现 ====================

// 结构：defer close(done) 必须是 runPushConsumer 的第一条语句——从而不存在
// 「参数校验段 panic 早于 defer 注册」导致 done 永不关闭、看门狗永远测不到的窗口。
func TestQA5_RunConsumerClosesDoneAsFirstStatement(t *testing.T) {
	const sig = "func runPushConsumer(ctx context.Context, c Config, gen int64, done chan struct{})"
	body := extractGoFunc(t, mustReadGoFile(t, "main.go"), sig)
	rest := body[len(sig):] // 跳过签名，避免命中签名里的 struct{} 花括号
	lb := strings.Index(rest, "{")
	if lb < 0 {
		t.Fatal("未找到函数体左花括号")
	}
	inner := strings.TrimSpace(rest[lb+1:])
	if !strings.HasPrefix(inner, "defer close(done)") {
		t.Fatalf("runPushConsumer 首条语句应为 defer close(done)（否则存在死亡窗口），实际以 %q 开头",
			firstLine(inner))
	}
}

// 行为：消费者 panic 死亡后，状态不得永久停在 running。
func TestQA5_DeadConsumerNotStuckRunning(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	pushGen.Store(5)
	if !setPushStateIfGen("running", "g5 运行中", 5) {
		t.Fatal("布置 running 状态失败")
	}
	pushConnect = func(context.Context, Config) (*CD2Client, *TokenInfo, error) {
		panic("qa5-die")
	}

	done := make(chan struct{})
	func() {
		defer func() { _ = recover() }()
		runPushConsumer(context.Background(),
			Config{EnablePush: true, Address: "127.0.0.1:1", Token: "x", PushDebounceSeconds: 1}, 5, done)
	}()

	if sn := pushSnapshot(); sn.State == "running" {
		t.Fatalf("消费者已死亡但状态仍停在 running：%+v", sn)
	}
	select {
	case <-done:
	default:
		t.Fatal("消费者死亡后 done 未关闭（看门狗将永远测不到）")
	}
}

// ==================== (e) 热重启：新世代状态不被旧的 off 覆盖 ====================

func TestQA5_HotRestartOldGenOffDropped(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	// 旧世代（1）已 running。
	pushGen.Store(1)
	if !setPushStateIfGen("running", "g1 运行中", 1) {
		t.Fatal("g1 running 写入失败")
	}
	// 热重启：世代前进到 2，新消费者进入 connecting。
	pushGen.Store(2)
	if !setPushStateIfGen("connecting", "g2 连接中", 2) {
		t.Fatal("g2 connecting 写入失败")
	}
	// 旧世代 g1 的非 ctx 退出对账写 off —— 必须被世代守卫丢弃（不得翻成 off）。
	if setPushStateIfGen("off", "订阅已退出：订阅结束", 1) {
		t.Fatal("旧世代 g1 的 off 写入应被丢弃")
	}
	if sn := pushSnapshot(); sn.State != "connecting" || sn.Gen != 2 {
		t.Fatalf("旧世代 off 覆盖了新世代状态：%+v（期望 connecting/gen2）", sn)
	}
	// 新消费者接管为 running。
	if !setPushStateIfGen("running", "g2 运行中", 2) {
		t.Fatal("g2 running 写入失败")
	}
	if sn := pushSnapshot(); sn.State != "running" || sn.Gen != 2 {
		t.Fatalf("新世代 running 未生效：%+v", sn)
	}
}

// 集成：保存配置（pushRev 变更）触发热重启后，必须以「更高世代」重新启动订阅。
func TestQA5_HotRestartRelaunchesWithNewGen(t *testing.T) {
	defer withTempPaths(t)()
	withPushGlobals(t)

	var mu sync.Mutex
	var gens []int64
	pushLaunch = func(_ context.Context, _ Config, gen int64, _ chan struct{}) {
		mu.Lock()
		gens = append(gens, gen)
		mu.Unlock()
	}
	genLen := func() int { mu.Lock(); defer mu.Unlock(); return len(gens) }

	stateMu.Lock()
	oldCfg := cfg
	cfg = Config{EnablePush: true, Address: "127.0.0.1:1", Token: "x", PushDebounceSeconds: 1, Tasks: []string{"/a"}}
	stateMu.Unlock()
	defer func() { stateMu.Lock(); cfg = oldCfg; stateMu.Unlock() }()

	ctx, cancel := context.WithCancel(context.Background())
	supDone := make(chan struct{})
	go func() { pushSupervisor(ctx); close(supDone) }()

	waitUntil(t, func() bool { return genLen() >= 1 }, 3*time.Second, "监督器未启动消费者")
	mu.Lock()
	g1 := gens[0]
	mu.Unlock()

	wakePushSupervisor() // 模拟「保存配置」→ pushRev 变更 → 热重启
	waitUntil(t, func() bool { return genLen() >= 2 }, 3*time.Second, "热重启未重新启动订阅")

	mu.Lock()
	g2 := gens[1]
	mu.Unlock()
	if g2 <= g1 {
		t.Fatalf("热重启后世代应递增：g1=%d g2=%d", g1, g2)
	}

	cancel()
	select {
	case <-supDone:
	case <-time.After(3 * time.Second):
		t.Fatal("监督器未随 ctx 取消退出")
	}
}

// ==================== (f) 前端存活呈现（静态锚点 + 内联 JS 安全） ====================

func TestQA5_FrontendLivenessMarkers(t *testing.T) {
	// 非运行中必显最近失败原因；运行中超 30 分钟无推送显黄色提示；阈值常量存在。
	must := []string{
		"function parseTS(",
		"function agoText(",
		"function durText(",
		"var PUSH_STALE_MS=30*60*1000",
		"if(st!=='running'&&lastPush.lastError){",
		"最近一次订阅失败：",
		"(Date.now()-anchor)>PUSH_STALE_MS",
		"订阅存活但已超过 30 分钟未收到任何推送",
		"var anchor=parseTS(lastPush.lastMessageAt)||parseTS(lastPush.subscribedAt)",
	}
	for _, m := range must {
		if !strings.Contains(pageHTML, m) {
			t.Fatalf("pageHTML 缺少存活可观测标记 %q", m)
		}
	}
	// 内联 <script> 区间不得含 Go 模板 {{（会破坏 node --check）。
	jsStart := strings.Index(pageHTML, "<script>")
	jsEnd := strings.LastIndex(pageHTML, "</script>")
	if jsStart < 0 || jsEnd < jsStart {
		t.Fatal("未找到内联 <script> 区间")
	}
	if strings.Contains(pageHTML[jsStart:jsEnd], "{{") {
		t.Fatal("内联 JS 出现 {{，会破坏 node --check")
	}
}

// ==================== (g) 风控红线：无周期性 ticker ====================

func TestQA5_NoPeriodicTickerInProduction(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		s := string(b)
		for _, bad := range []string{"time.NewTicker(", "time.Tick("} {
			if strings.Contains(s, bad) {
				t.Fatalf("生产文件 %s 含周期性 ticker %q（触碰「禁止每 N 秒遍历」风控红线）", name, bad)
			}
		}
	}
	if checked == 0 {
		t.Fatal("未枚举到任何生产 .go 文件（路径断言失效）")
	}
	// 看门狗本体必须是纯控制面判断：不得含目录遍历。
	wd := extractGoFunc(t, mustReadGoFile(t, "main.go"), "func pushWatchdogLocked() (deadGen int64, dead bool)")
	for _, bad := range []string{"filepath.Walk", "scanDir", "walkDir", "List("} {
		if strings.Contains(wd, bad) {
			t.Fatalf("pushWatchdogLocked 含目录遍历关键字 %q（看门狗应只做控制面判断）", bad)
		}
	}
}

// firstLine 返回字符串的首行（用于报错信息）。
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
