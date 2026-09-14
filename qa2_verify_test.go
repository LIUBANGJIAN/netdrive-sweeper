package main

// qa2_verify_test.go —— 第二层独立验证（QA #2）。
// 目标：不复述实现者结论，用怀疑视角独立构造反例/边界/并发压力测试，
// 并对三项用户需求逐一取证。所有测试名以 TestQA2_ 前缀，避免与既有用例冲突。

import (
	"context"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ==================== A. 防抖上限与触发正确性（push.go） ====================

// A-负: maxDelay 默认 = 6×debounce；debounce<=0 回退 5s。
func TestQA2_MaxDelayDefaults(t *testing.T) {
	cases := []struct {
		in       time.Duration
		debounce time.Duration
		maxDelay time.Duration
	}{
		{5 * time.Second, 5 * time.Second, 30 * time.Second},
		{2 * time.Second, 2 * time.Second, 12 * time.Second},
		{0, 5 * time.Second, 30 * time.Second},
		{-3 * time.Second, 5 * time.Second, 30 * time.Second},
	}
	for _, c := range cases {
		p := newPushConsumer(nil, c.in, nil)
		if p.debounce != c.debounce || p.maxDelay != c.maxDelay {
			t.Fatalf("debounce=%v => got debounce=%v maxDelay=%v, want %v/%v",
				c.in, p.debounce, p.maxDelay, c.debounce, c.maxDelay)
		}
	}
}

// A-正: 单事件必须触发一次。
func TestQA2_SingleEventTriggersOnce(t *testing.T) {
	var n int64
	p := newPushConsumer(nil, 20*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
	p.log = func(string, ...any) {}
	p.handle(4)
	time.Sleep(8 * p.debounce)
	if got := atomic.LoadInt64(&n); got != 1 {
		t.Fatalf("单事件应恰好触发 1 次，实际 %d", got)
	}
}

// A-边界: 有界突发（全部落在 maxDelay 内）必须被合并为「恰好 1 次」。
// 注意：Windows 的 time.Sleep(1ms) 实际会睡 ~15ms，若用细粒度 sleep 灌事件，
// 突发跨度可能超过 maxDelay 而「按设计」产生多次触发。故这里用：
//
//	(1) 紧凑循环（无 sleep，跨度微秒级）；(2) 大 debounce 拉高 maxDelay 容错。
func TestQA2_FiniteBurstExactlyOneTrigger(t *testing.T) {
	for iter := 0; iter < 8; iter++ {
		var n int64
		p := newPushConsumer(nil, 200*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
		p.log = func(string, ...any) {}
		// 紧凑突发：200 个事件，跨度 << maxDelay(1200ms)，绝不会触及上限。
		for i := 0; i < 200; i++ {
			p.handle(4)
		}
		time.Sleep(p.debounce + 150*time.Millisecond)
		if got := atomic.LoadInt64(&n); got != 1 {
			t.Fatalf("iter=%d: 一次突发应合并为恰好 1 次触发，实际 %d", iter, got)
		}
	}
	// 变体：带 sleep 但用足够大的 debounce，确保跨度仍在 maxDelay 之内（免受时钟粒度影响）。
	for iter := 0; iter < 6; iter++ {
		var n int64
		p := newPushConsumer(nil, 300*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
		p.log = func(string, ...any) {}
		for i := 0; i < 8; i++ { // 8×~15ms≈120ms << maxDelay 1800ms
			p.handle(4)
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(p.debounce + 200*time.Millisecond)
		if got := atomic.LoadInt64(&n); got != 1 {
			t.Fatalf("iter=%d(带sleep): 一次突发应合并为恰好 1 次触发，实际 %d", iter, got)
		}
	}
}

// A-并发压力: 多 goroutine 同时灌事件（合并同一突发），仍须「恰好 1 次」。
// 用于暴露 handle/schedule 的并发双触发或漏触发。
func TestQA2_ConcurrentBurstExactlyOneTrigger(t *testing.T) {
	for iter := 0; iter < 12; iter++ {
		var n int64
		p := newPushConsumer(nil, 30*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
		p.log = func(string, ...any) {}
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 40; i++ {
					p.handle(4)
				}
			}()
		}
		wg.Wait()
		time.Sleep(10 * p.debounce)
		if got := atomic.LoadInt64(&n); got != 1 {
			t.Fatalf("iter=%d: 并发突发应合并为恰好 1 次触发，实际 %d", iter, got)
		}
	}
}

// A-核心竞态: 「达上限的立即触发」与「末次事件的定时器」同时到达时，只能触发 1 次。
// 直接驱动 fire()：先布置好待触发突发 + 一个挂起的定时器，再让多个 goroutine 并发 fire()，
// 之后挂起定时器也会回调 fire()。断言全部路径合起来恰好 1 次。
func TestQA2_CappedFireVsPendingTimerExactlyOne(t *testing.T) {
	for iter := 0; iter < 20; iter++ {
		var n int64
		p := newPushConsumer(nil, 40*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
		p.log = func(string, ...any) {}

		// 布置：已有一个进行中的突发 + 一个即将触发的定时器。
		p.mu.Lock()
		p.firstEventAt = time.Now()
		p.mu.Unlock()
		p.schedule(3 * time.Millisecond)

		// 并发调用 fire()（模拟「达到上限的立即触发」与定时器回调同时到达）。
		var wg sync.WaitGroup
		for g := 0; g < 6; g++ {
			wg.Add(1)
			go func() { defer wg.Done(); p.fire() }()
		}
		wg.Wait()
		time.Sleep(3 * p.debounce) // 让挂起定时器（3ms）也回调 fire()

		if got := atomic.LoadInt64(&n); got != 1 {
			t.Fatalf("iter=%d: 立即触发与末次定时器叠加应恰好 1 次，实际 %d", iter, got)
		}
	}
}

// A-防饿死: 持续不断的事件流下，必须在 maxDelay 内触发（否则即「防抖饥饿」回归）。
// 同时断言：触发不会「瞬时双响」（相邻两次触发间隔 >= debounce），且无失控刷触发。
func TestQA2_ContinuousStreamFiresWithinCap(t *testing.T) {
	for iter := 0; iter < 12; iter++ {
		debounce := 20 * time.Millisecond
		var mu sync.Mutex
		var times []time.Duration
		start := time.Now()
		p := newPushConsumer(nil, debounce, func() {
			mu.Lock()
			times = append(times, time.Since(start))
			mu.Unlock()
		})
		p.log = func(string, ...any) {}

		stop := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk := time.NewTicker(2 * time.Millisecond)
			defer tk.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tk.C:
					p.handle(4)
				}
			}
		}()

		time.Sleep(150 * time.Millisecond) // maxDelay=120ms，已越过上限
		close(stop)
		wg.Wait()
		time.Sleep(10 * debounce) // 收尾

		mu.Lock()
		got := append([]time.Duration(nil), times...)
		mu.Unlock()

		if len(got) == 0 {
			t.Fatalf("iter=%d: 持续事件流下从未触发 → 防抖饥饿回归", iter)
		}
		if got[0] > p.maxDelay+4*debounce {
			t.Fatalf("iter=%d: 首次触发 %v 超出 maxDelay(%v)+余量 → 上限未生效", iter, got[0], p.maxDelay)
		}
		for i := 1; i < len(got); i++ {
			if got[i]-got[i-1] < debounce {
				t.Fatalf("iter=%d: 相邻触发间隔 %v < debounce(%v) → 瞬时双触发", iter, got[i]-got[i-1], debounce)
			}
		}
		if len(got) > 4 {
			t.Fatalf("iter=%d: 触发次数失控 = %d", iter, len(got))
		}
	}
}

// A-重触发: rearm() 后确实再触发一次，且不会无限自我重排（无新事件时停止）。
func TestQA2_RearmRetriggersOnceThenStops(t *testing.T) {
	var n int64
	p := newPushConsumer(nil, 25*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
	p.log = func(string, ...any) {}

	p.handle(4)
	time.Sleep(6 * p.debounce)
	p.rearm()
	time.Sleep(6 * p.debounce)
	if got := atomic.LoadInt64(&n); got != 2 {
		t.Fatalf("handle+rearm 应触发 2 次，实际 %d", got)
	}
	// rearm 只重排一次；不再 rearm 则不得继续触发。
	time.Sleep(20 * p.debounce)
	if got := atomic.LoadInt64(&n); got != 2 {
		t.Fatalf("rearm 不应无限自我重排，实际 %d 次", got)
	}
}

// A-有界重试: 模拟「每轮触发都 rearm（扫描始终互斥）」的场景，确认次数由调用方条件终止。
func TestQA2_RearmBoundedRetryLoop(t *testing.T) {
	var n int64
	var p *pushConsumer
	p = newPushConsumer(nil, 15*time.Millisecond, func() {
		c := atomic.AddInt64(&n, 1)
		if c < 3 { // 前两轮「扫描互斥」，第三轮结束
			p.rearm()
		}
	})
	p.log = func(string, ...any) {}
	p.handle(4)
	time.Sleep(60 * p.debounce)
	if got := atomic.LoadInt64(&n); got != 3 {
		t.Fatalf("rearm 重试应由条件终止于 3 次，实际 %d", got)
	}
}

// A-漏触发: 两次事件间隔明显大于 debounce 时，应分别触发（证明事件不会被吞）。
func TestQA2_GapExceedsDebounceTriggersTwice(t *testing.T) {
	var n int64
	p := newPushConsumer(nil, 25*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
	p.log = func(string, ...any) {}
	p.handle(4)
	time.Sleep(5 * p.debounce)
	p.handle(4)
	time.Sleep(5 * p.debounce)
	if got := atomic.LoadInt64(&n); got != 2 {
		t.Fatalf("间隔>debounce 的两次事件应触发 2 次，实际 %d", got)
	}
}

// A-定向回归(锁定上一轮报告的窗口): handle 的「达上限立即触发」路径在存在挂起定时器时，
// 必须在一次持锁内完成 清空突发状态 + 停表 + 异步触发；随后挂起定时器的 fire() 回调也不得重复触发。
func TestQA2_HandleCappedPathExactlyOne(t *testing.T) {
	for iter := 0; iter < 30; iter++ {
		var n int64
		p := newPushConsumer(nil, 20*time.Millisecond, func() { atomic.AddInt64(&n, 1) })
		p.log = func(string, ...any) {}
		// 布置：进行中的突发已超过 maxDelay（下一次 handle 必走「立即触发」分支）+ 一个挂起定时器。
		p.mu.Lock()
		p.firstEventAt = time.Now().Add(-2 * p.maxDelay)
		p.mu.Unlock()
		p.schedule(30 * time.Millisecond) // 挂起定时器（远晚于 handle，不会先行 fire）

		p.handle(4)                        // 走 capped 分支：锁内清空 + 停表 + asyncTrigger
		time.Sleep(100 * time.Millisecond) // 让 asyncTrigger 与被停掉的定时器都有机会运行

		if got := atomic.LoadInt64(&n); got != 1 {
			t.Fatalf("iter=%d: capped 分支叠加挂起定时器应恰好 1 次触发，实际 %d", iter, got)
		}
	}
}

// A-原子性锚定(结构): handle 必须在一次持锁内完成「决策 + 定时器创建/立即触发」——
// 结构上不得再调用取锁版 schedule()，capped 分支必须锁内停表并异步触发；rearm 同理。
func TestQA2_HandleAtomicScheduleLocked(t *testing.T) {
	b, err := os.ReadFile("push.go")
	if err != nil {
		t.Fatalf("读取 push.go: %v", err)
	}
	h := extractGoFunc(t, string(b), "func (p *pushConsumer) handle(messageType int32)")
	if !strings.Contains(h, "scheduleLocked(") {
		t.Fatal("handle 未使用 scheduleLocked（应同一持锁内重建定时器）")
	}
	if strings.Contains(h, "p.schedule(") {
		t.Fatal("handle 仍调用取锁版 schedule()（解锁后再 schedule 的竞态窗口可能回归）")
	}
	if !strings.Contains(h, "p.firstEventAt = time.Time{}") || !strings.Contains(h, "p.timer.Stop()") {
		t.Fatal("handle 的 capped 分支未在锁内清空突发状态并停止定时器")
	}
	r := extractGoFunc(t, string(b), "func (p *pushConsumer) rearm()")
	if !strings.Contains(r, "scheduleLocked(") || strings.Contains(r, "p.schedule(") {
		t.Fatal("rearm 未在锁内使用 scheduleLocked")
	}
}

// ==================== B. keepalive 参数锚定（cd2client.go） ====================

// B: 用源文件锚定 keepalive 三参数，防回归（grpc 连接内部不可反射，改以源码断言）。
func TestQA2_KeepaliveParamsAnchoredInSource(t *testing.T) {
	b, err := os.ReadFile("cd2client.go")
	if err != nil {
		t.Fatalf("读取 cd2client.go: %v", err)
	}
	s := string(b)
	checks := map[string]*regexp.Regexp{
		"keepalive 参数块":             regexp.MustCompile(`grpc\.WithKeepaliveParams\(keepalive\.ClientParameters\{`),
		"Time=60s":                  regexp.MustCompile(`Time:\s*60\s*\*\s*time\.Second`),
		"Timeout=20s":               regexp.MustCompile(`Timeout:\s*20\s*\*\s*time\.Second`),
		"PermitWithoutStream=false": regexp.MustCompile(`PermitWithoutStream:\s*false`),
	}
	for name, re := range checks {
		if !re.MatchString(s) {
			t.Fatalf("cd2client.go 缺少 keepalive 参数锚点[%s]（正则 %q）", name, re.String())
		}
	}
	// PermitWithoutStream 必须为 false：若为 true，空闲期会无脑 ping → CD2 侧 too_many_pings GOAWAY。
	if regexp.MustCompile(`PermitWithoutStream:\s*true`).MatchString(s) {
		t.Fatal("PermitWithoutStream 不应为 true")
	}
}

// ==================== C. statusMonitor 反回归与真实重试证据 ====================

// C-反回归(风控红线): statusMonitor 调用链（statusMonitor→probeCD2Status→connectPushClient）
// 只能出现 TCPCheck + TokenInfo，绝不能出现目录遍历 List(/scanDir。
func TestQA2_StatusMonitorPathHasNoDirectoryScan(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读取 main.go: %v", err)
	}
	body := extractGoFunc(t, string(b), "func statusMonitor(ctx context.Context)")
	// statusMonitor 及其被调用的辅助函数必须不含目录遍历。
	aux := extractGoFunc(t, string(b), "func probeCD2Status(ctx context.Context, c Config)")
	chain := body + "\n" + aux
	for _, bad := range []string{"List(", "scanDir", "walkDir", "filepath.Walk"} {
		if strings.Contains(chain, bad) {
			t.Fatalf("statusMonitor 调用链出现目录遍历/全树扫描关键字 %q → 触碰风控红线", bad)
		}
	}
	// 正向：必须使用 TCPCheck + TokenInfo。
	conn := extractGoFunc(t, string(b), "func connectPushClient(ctx context.Context, c Config)")
	for _, want := range []string{"TCPCheck", "TokenInfo"} {
		if !strings.Contains(conn, want) {
			t.Fatalf("connectPushClient 应包含 %q", want)
		}
	}
}

// C-反回归: 旧 statusProbeLoop 必须彻底删除，源码中无残留引用。
func TestQA2_StatusProbeLoopRemoved(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取目录: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if e.Name() == "qa2_verify_test.go" { // 本文件含该词的字面量，跳过自身
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("读取 %s: %v", e.Name(), err)
		}
		if strings.Contains(string(b), "statusProbeLoop") {
			t.Fatalf("%s 仍残留 statusProbeLoop 引用", e.Name())
		}
	}
}

// C-真实调用路径: runPushConsumer 的 denied 分支必须「调用 markPushDeniedConnected」且
// 「不再永久 return」（含 60s 退避重试）。以源码块扫描锚定。
func TestQA2_RunPushConsumerDeniedBranch(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读取 main.go: %v", err)
	}
	body := extractGoFunc(t, string(b), "func runPushConsumer(ctx context.Context, c Config)")
	// 截取 denied 分支片段。
	i := strings.Index(body, "!token.AllowPushMessage")
	if i < 0 {
		t.Fatal("runPushConsumer 内未找到 !token.AllowPushMessage 分支")
	}
	seg := body[i:]
	if j := strings.Index(seg, "setStatus(\"已连接: \"+token.RootDir, token)"); j >= 0 {
		seg = seg[:j]
	}
	if !strings.Contains(seg, "markPushDeniedConnected(token)") {
		t.Fatal("denied 分支未在真实路径调用 markPushDeniedConnected(token) → 徽章不会自动点亮")
	}
	if !strings.Contains(seg, "waitOrDone(ctx, 60*time.Second)") {
		t.Fatal("denied 分支未做 60s 退避重试 → 用户勾选权限后不能自愈")
	}
	if regexp.MustCompile(`(?m)^\s*return\s*$`).MatchString(seg) {
		// denied 分支在 waitOrDone 前后不应出现无条件 return（末尾 return 由 waitOrDone 判定）。
		// 仅当 return 早于 markPushDeniedConnected 时才判定为「永久 return」回归。
		ri := strings.Index(seg, "return")
		mi := strings.Index(seg, "markPushDeniedConnected(token)")
		if ri >= 0 && ri < mi {
			t.Fatal("denied 分支在写状态前就 return → 徽章不亮的回归")
		}
	}
}

// C-行为: 未配置地址/Token 时 statusMonitor 不 return，而是持续重试（可被 ctx 取消）。
func TestQA2_StatusMonitorUnconfiguredKeepsRetrying(t *testing.T) {
	stateMu.Lock()
	oldCfg := cfg
	cfg = Config{} // 空地址 + 空 Token
	stateMu.Unlock()
	defer func() { stateMu.Lock(); cfg = oldCfg; stateMu.Unlock() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { statusMonitor(ctx); close(done) }()

	time.Sleep(200 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("未配置时 statusMonitor 不应 return（应持续重试等待用户补配置）")
	default:
	}
	stateMu.Lock()
	msg := statusInfo.LastMessage
	stateMu.Unlock()
	if msg != "未配置 CD2 地址或 Token" {
		t.Fatalf("未配置时应写入提示，实际 %q", msg)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel 后 statusMonitor 未退出")
	}
}

// C-行为: 用「本地可达但非 gRPC」的监听器，证明 statusMonitor 会真的一遍遍重试（连续 TCP 探测），
// 且失败期间不会每轮刷日志。风控红线：只发生 TCP 连接，无目录遍历请求。
func TestQA2_StatusMonitorRetriesContinuously(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	var accepts int64
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt64(&accepts, 1)
			_ = c.Close()
		}
	}()

	tmp, err := os.CreateTemp("", "qa2_monitor_*.log")
	if err != nil {
		t.Fatalf("temp log: %v", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	stateMu.Lock()
	oldCfg := cfg
	oldLog := logPath
	cfg = Config{Address: ln.Addr().String(), Token: "qa2-dummy"}
	logPath = tmpPath
	stateMu.Unlock()
	defer func() {
		stateMu.Lock()
		cfg = oldCfg
		logPath = oldLog
		stateMu.Unlock()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { statusMonitor(ctx); close(done) }()

	// 首次探测在 t≈0；退避 3s 后进行第二次探测。窗口取 4.5s 足以覆盖两次探测。
	time.Sleep(4500 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel 后 statusMonitor 未退出（goroutine 泄漏）")
	}

	if got := atomic.LoadInt64(&accepts); got < 2 {
		t.Fatalf("不可达/非 gRPC 目标下应持续重试（期望 >=2 次 TCP 探测），实际 %d —— 疑似放弃重试", got)
	}

	logb, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("读取日志: %v", err)
	}
	logs := string(logb)
	if !strings.Contains(logs, "状态自检") {
		t.Fatalf("应至少产生一条状态自检日志，实际: %q", logs)
	}
	nUnreach := strings.Count(logs, "暂不可达")
	if nUnreach > 2 {
		t.Fatalf("失败重试期间不应刷屏：出现 %d 条「暂不可达」，期望 <=2", nUnreach)
	}
}

// ==================== D. 前端反回归（web.go） ====================

// D: 页面不得再有秒级计时 / tickRun / 全屏遮罩显示逻辑；运行期必须 1200ms 轮询且 finally 清理。
func TestQA2_FrontendNoElapsedNoOverlay(t *testing.T) {
	page := pageHTML
	// 反回归：旧计时与遮罩显示逻辑必须彻底消失。
	// 注：静态 <span id="busyText"> 仍在 HTML 中（父 #busy 默认 display:none 且无任何 JS 引用，
	// 属无害残留）；此处只断言「JS 不再引用/显示遮罩」，JS 级引用由 TestQA2_BusyOverlayNeverShown 覆盖。
	for _, bad := range []string{
		"已耗时", "tickRun", "runTimer", "runStart",
		"el('busy')", "el(\"busy\")", "el('busy').style.display",
	} {
		if strings.Contains(page, bad) {
			t.Fatalf("pageHTML 不应再包含 %q", bad)
		}
	}
	// 正向：运行期高频轮询必须在位（1200ms）。
	if !strings.Contains(page, "runPollTimer=setInterval(function(){loadLogs().catch(function(){})},1200)") {
		t.Fatal("pageHTML 缺少运行期 1200ms 日志轮询")
	}
	// 8s 常规轮询必须在运行期让位。
	if !strings.Contains(page, "logState.follow&&!runPollTimer") {
		t.Fatal("pageHTML 的 8s 常规轮询未在运行期让位（可能重复请求）")
	}
	// 运行期定时器必须在 finally 中清理（异常路径也覆盖）。
	if !strings.Contains(page, "stopRunPolling();endRun()") {
		t.Fatal("pageHTML 的 .finally 未清理运行期定时器")
	}
	if !strings.Contains(page, "function stopRunPolling(){") {
		t.Fatal("pageHTML 未定义 stopRunPolling")
	}
}

// D: #busy 元素虽残留在 HTML/CSS，但不得被任何 JS 显示——否则 endRun 不再隐藏会永久卡死遮罩。
func TestQA2_BusyOverlayNeverShown(t *testing.T) {
	page := pageHTML
	// 找到 <script>...</script> 区间，只在该区间内检查对 busy 的动态引用。
	jsStart := strings.Index(page, "<script>")
	jsEnd := strings.LastIndex(page, "</script>")
	if jsStart < 0 || jsEnd < jsStart {
		t.Fatal("未找到内联 <script> 区间")
	}
	js := page[jsStart:jsEnd]
	if strings.Contains(js, "busy") || strings.Contains(js, "busyText") {
		t.Fatalf("内联 JS 仍引用 busy 相关元素，存在遮罩卡死风险: %s", js)
	}
	// 静态元素/CSS 若仍存在，必须默认为 display:none。
	if strings.Contains(page, "id=\"busy\"") && !strings.Contains(page, "#busy{position:fixed") {
		t.Fatal("#busy 元素存在但未见默认隐藏样式")
	}
}

// D-新: #busy 相关元素/CSS/变量与 .spinner.dark 必须彻底删除；.spinner 基类仍需被 #runBtn 使用。
func TestQA2_WebBusyResidueRemoved(t *testing.T) {
	for _, bad := range []string{`id="busy"`, "busyText", "busy-inner", "--z-busy", ".spinner.dark"} {
		if strings.Contains(pageHTML, bad) {
			t.Fatalf("pageHTML 仍残留遮罩/死样式痕迹 %q", bad)
		}
	}
	if !strings.Contains(pageHTML, ".spinner{") {
		t.Fatal("缺少 .spinner 基类样式（#runBtn 内联 spinner 依赖它）")
	}
	if !strings.Contains(pageHTML, `<span class="spinner">`) {
		t.Fatal(`startRun 未再给 #runBtn 注入 <span class="spinner">`)
	}
}

// ==================== 辅助函数 ====================

// extractGoFunc 从 src 中截取以 sig 开头、到下一个顶层 "func " 之前的函数体。
func extractGoFunc(t *testing.T, src, sig string) string {
	t.Helper()
	i := strings.Index(src, sig)
	if i < 0 {
		t.Fatalf("未找到函数签名 %q", sig)
	}
	rest := src[i:]
	if j := strings.Index(rest[len(sig):], "\nfunc "); j >= 0 {
		return rest[:len(sig)+j]
	}
	return rest
}
