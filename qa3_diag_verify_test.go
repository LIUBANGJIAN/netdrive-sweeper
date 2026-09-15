package main

// qa3_diag_verify_test.go —— 第二层独立验证（针对 d2cf965「事件驱动可诊断性 + 健壮性」改造）。
// 与实现者的 eventdriven_diag_test.go 相互独立：这里从对抗性视角出发，覆盖
// 「未知字段解析的对抗输入 / 真实重连(退避+EOF 区分)行为 / 自检链路的节流与风控红线 / 前端字段缺失兼容」。

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ==================== B. protobuf 未知字段解析：对抗输入 ====================

// B-1: 合法 10 字节 varint 必须成功解码（n=10）；>10 字节安全失败为 (0,0)。
func TestQA3v_DecodeVarintTenByteOK(t *testing.T) {
	in := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
	if v, n := decodeVarint(in); n != 10 {
		t.Fatalf("10 字节 varint 应成功解码 n=10，实际 n=%d v=%d", n, v)
	}
	over := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
	if v, n := decodeVarint(over); n != 0 || v != 0 {
		t.Fatalf(">10 字节 varint 应安全失败 (0,0)，实际 (%d,%d)", v, n)
	}
}

// B-2: nestedBytes 仅在「字段号=5 且 wire type=2」时取值；wire 不匹配或字段缺失都必须返回 nil。
func TestQA3v_NestedBytesNoField5NoMisuse(t *testing.T) {
	if got := nestedBytes(parseRawFields(pbVarintField(5, 7)), 5); got != nil {
		t.Fatalf("field5 wire=0 不应被当作嵌套消息，实际 %v", got)
	}
	topOther := append(pbBytesField(6, []byte("/should/not/be/picked")), pbBytesField(7, []byte("x"))...)
	if got := nestedBytes(parseRawFields(topOther), 5); got != nil {
		t.Fatalf("字段 5 缺失时不应误取其它 length-delimited 字段，实际 %v", got)
	}
	if ev := buildFileSystemChangeEvent(fileSystemChangeType, topOther); ev.Path != "" {
		t.Fatalf("字段 5 缺失时 Path 应为空，实际 %q", ev.Path)
	}
}

// B-3: 对抗性 wire 字节——截断 / 声明长度超实际 / 非法 wire / 巨大字段号——绝不 panic。
func TestQA3v_ParseRawFieldsAdversarial(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		pbBytesField(5, []byte("abc"))[:2], // 截断：声明长度 3 但只剩 1 字节
		append(pbBytesField(5, []byte("ok")), 0x7a), // 正常字段 + 悬空 tag
		{0x3d},                               // field7 wire5，缺 4 字节
		{0x39},                               // field7 wire1，缺 8 字节
		{0x0b},                               // field1 wire3（group）
		{0x0c},                               // field1 wire4（end-group）
		{0x0e},                               // field1 wire6（非法）
		{0x0f},                               // field1 wire7（非法）
		{0xff, 0xff, 0xff, 0xff, 0x0f, 0x00}, // 极大字段号
		append(pbBytesField(1, []byte("a")), []byte{0x08, 0xff}...), // 未终止 varint
	}
	for i, b := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("case %d: parseRawFields(%v) panic: %v", i, b, r)
				}
			}()
			_ = parseRawFields(b)
			if ev := buildFileSystemChangeEvent(fileSystemChangeType, b); ev.Type != fileSystemChangeType {
				t.Fatalf("case %d: Type 必须保留，实际 %d", i, ev.Type)
			}
		}()
	}
}

// B-4: 多个 length-delimited 字符串时，bestPathCandidate 属「尽力而为」——
// 只允许返回其中之一（绝不凭空造值），并显式暴露「可能把 oldPath 当 path」的歧义。
func TestQA3v_MultiStringBestEffort(t *testing.T) {
	inner := append(
		pbBytesField(3, []byte("/old/very/deep/oldfile.mkv")),
		pbBytesField(2, []byte("/short"))...,
	)
	ev := buildFileSystemChangeEvent(fileSystemChangeType, pbBytesField(5, inner))
	allowed := map[string]bool{"/old/very/deep/oldfile.mkv": true, "/short": true, "": true}
	if !allowed[ev.Path] {
		t.Fatalf("尽力而为路径必须是候选之一，实际凭空产出 %q", ev.Path)
	}
	if ev.Path != "/old/very/deep/oldfile.mkv" {
		t.Logf("本次选中 %q（若 CD2 同时携带 oldPath，存在把 oldPath 误标为 path 的风险）", ev.Path)
	}
}

// B-5: printableText 边界：非 UTF-8 / 控制字符拒绝；tab 允许；DEL(0x7f) 允许（仅诊断用）。
func TestQA3v_PrintableTextBoundary(t *testing.T) {
	if printableText([]byte{0xef, 0xbf, 0xbd}) == "" {
		t.Fatal("合法 UTF-8（替换符）应被接受")
	}
	if printableText([]byte{0x00}) != "" || printableText([]byte{0x1f}) != "" {
		t.Fatal("控制字符应被拒绝")
	}
	if printableText([]byte("a\tb")) != "a\tb" {
		t.Fatal("tab 应被允许")
	}
	if printableText([]byte{'a', 0x7f, 'b'}) == "" {
		t.Fatal("DEL(0x7f) 目前被允许（仅诊断用），行为如预期")
	}
}

// ==================== C. 真实重连行为（fake gRPC server） ====================

// C-1: 服务端 Unimplemented（error）→ run() 记「正在建立…第 N 次」「订阅中断（第 N 次）」，N 严格递增。
func TestQA3v_RunLogsInterruptAndBackoff(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer() // 未注册 service → PushMessage 返回 Unimplemented
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	client, err := newCD2Client(Config{Address: lis.Addr().String(), Token: "x"})
	if err != nil {
		t.Fatalf("newCD2Client: %v", err)
	}
	defer client.Close()

	joined := strings.Join(captureRunLogs(t, client, 7*time.Second), "\n")
	if !strings.Contains(joined, "正在建立 PushMessage 订阅（第 1 次）") {
		t.Fatalf("缺少第 1 次建立日志：\n%s", joined)
	}
	if !strings.Contains(joined, "订阅中断（第 1 次）") {
		t.Fatalf("缺少第 1 次中断日志：\n%s", joined)
	}
	if strings.Contains(joined, "订阅已结束") {
		t.Fatalf("error 场景不得落入「订阅已结束」(EOF) 分支：\n%s", joined)
	}
	nums := attemptNumbers(t, joined)
	if len(nums) < 3 {
		t.Fatalf("7s 内按 2s/4s 退避应至少尝试 3 次，实际 %v\n%s", nums, joined)
	}
	for i := 1; i < len(nums); i++ {
		if nums[i] <= nums[i-1] {
			t.Fatalf("重连次数未严格递增（忙重连/退避失效）：%v", nums)
		}
	}
}

// C-2: 服务端优雅关闭(EOF) → run() 走「订阅已结束」分支，与 error 分支可区分。
func TestQA3v_RunLogsGracefulEOF(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	handler := func(_ interface{}, stream grpc.ServerStream) error {
		var req emptypb.Empty
		_ = stream.RecvMsg(&req)
		return nil
	}
	srv := grpc.NewServer(grpc.UnknownServiceHandler(handler))
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	client, err := newCD2Client(Config{Address: lis.Addr().String(), Token: "x"})
	if err != nil {
		t.Fatalf("newCD2Client: %v", err)
	}
	defer client.Close()

	joined := strings.Join(captureRunLogs(t, client, 3*time.Second), "\n")
	if !strings.Contains(joined, "订阅已结束") {
		t.Fatalf("EOF（服务端优雅关闭）应记「订阅已结束」：\n%s", joined)
	}
	if strings.Contains(joined, "订阅中断") {
		t.Fatalf("EOF 不应记「订阅中断」：\n%s", joined)
	}
}

func captureRunLogs(t *testing.T, client *CD2Client, d time.Duration) []string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	p := newPushConsumer(client, 50*time.Millisecond, nil)
	p.log = func(format string, args ...any) {
		mu.Lock()
		lines = append(lines, fmt.Sprintf(format, args...))
		mu.Unlock()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.run(ctx); close(done) }()
	time.Sleep(d)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("run 未在 ctx 取消后退出（goroutine 泄漏）")
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), lines...)
}

var attemptRe = regexp.MustCompile(`正在建立 PushMessage 订阅（第 (\d+) 次）`)

func attemptNumbers(t *testing.T, s string) []int {
	t.Helper()
	ms := attemptRe.FindAllStringSubmatch(s, -1)
	nums := make([]int, 0, len(ms))
	for _, m := range ms {
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
			t.Fatalf("解析尝试次数 %q 失败: %v", m[1], err)
		}
		nums = append(nums, n)
	}
	return nums
}

// ==================== D. 自检链路：节流 + 风控红线 ====================

// D-1: 自检链路整条不得出现目录遍历（风控红线）。
func TestQA3v_StatusMonitorChainNoDirectoryScan(t *testing.T) {
	src := mustReadGoFile(t, "main.go")
	funcs := []string{
		"func statusMonitor(ctx context.Context)",
		"func probeCD2Status(ctx context.Context, c Config)",
		"func connectPushClient(ctx context.Context, c Config)",
		"func checkCloudEventListeners(ctx context.Context, c Config, lastKey string)",
		"func cloudListenerReport(apis []CloudAPI, pushLive bool)",
	}
	for _, sig := range funcs {
		body := extractGoFunc(t, src, sig)
		for _, bad := range []string{"List(", "scanDir", "walkDir", "filepath.Walk"} {
			if strings.Contains(body, bad) {
				t.Fatalf("%s 体出现目录遍历关键字 %q → 触碰风控红线", sig, bad)
			}
		}
	}
}

// D-2: 云端监听器体检必须受节流控制（翻转立即 + 缓存窗口），不得每次探测都查。
func TestQA3v_CloudListenerThrottleStructure(t *testing.T) {
	if cloudListenerInterval != 2*time.Minute {
		t.Fatalf("cloudListenerInterval 应为 2min，实际 %v", cloudListenerInterval)
	}
	body := extractGoFunc(t, mustReadGoFile(t, "main.go"), "func statusMonitor(ctx context.Context)")
	if !strings.Contains(body, "cloudListenerInterval") {
		t.Fatal("statusMonitor 未使用 cloudListenerInterval 节流")
	}
	if !strings.Contains(body, `flipped || listenerKey == "" || time.Since(listenerCheckedAt) >= cloudListenerInterval`) {
		t.Fatal("statusMonitor 的体检触发条件不是「翻转立即 + 缓存窗口」")
	}
	if n := strings.Count(body, "checkCloudEventListeners("); n != 1 {
		t.Fatalf("statusMonitor 中 checkCloudEventListeners 调用次数应为 1，实际 %d", n)
	}
}

// D-3: cloudListenerReport 的 key 不变/变；空列表安全（key 现含 pushLive 证据位）。
func TestQA3v_CloudListenerReportKeyEdges(t *testing.T) {
	lines, key := cloudListenerReport(nil, false)
	if len(lines) != 0 {
		t.Fatalf("空列表应得空行，实际 lines=%v", lines)
	}
	// key 现在包含 pushLive 证据位，故空列表的 key 为 "pushLive=false"（非空）。
	if key != "pushLive=false" {
		t.Fatalf("空列表 key 应为 pushLive=false，实际 %q", key)
	}
	_, k1 := cloudListenerReport([]CloudAPI{{Name: "115", IsCloudEventListenerRunning: false}}, false)
	_, k1b := cloudListenerReport([]CloudAPI{{Name: "115", IsCloudEventListenerRunning: false}}, false)
	if k1 != k1b {
		t.Fatalf("相同结果 key 不稳定：%q vs %q", k1, k1b)
	}
	_, k2 := cloudListenerReport([]CloudAPI{{Name: "115", IsCloudEventListenerRunning: true}}, false)
	if k2 == k1 {
		t.Fatalf("结果变化 key 未改变：%q", k2)
	}
}

// D-3b: 云端监听器体检查询失败时必须「静默降级」——原样返回 lastKey，不误报、不清空。
func TestQA3v_CloudListenerQueryFailSilentDegrade(t *testing.T) {
	const last = "115=true"
	// 指向必然拒绝连接的地址：查询失败 → 必须原样返回 lastKey（不得变成空签名而触发误报）。
	got := checkCloudEventListeners(context.Background(), Config{
		Address: "127.0.0.1:1", Token: "x",
	}, last)
	if got != last {
		t.Fatalf("查询失败应静默降级返回 lastKey=%q，实际 %q（可能清空/误报）", last, got)
	}
	// 结构上确认：两条错误分支均 return lastKey（newCD2Client 失败 / CloudAPIs 失败）。
	body := extractGoFunc(t, mustReadGoFile(t, "main.go"), "func checkCloudEventListeners(ctx context.Context, c Config, lastKey string) string")
	if n := strings.Count(body, "return lastKey"); n < 2 {
		t.Fatalf("checkCloudEventListeners 错误分支应至少 2 处 return lastKey（静默降级），实际 %d", n)
	}
}

// D-4: cd2.proto 新增类型可被运行时解析；CloudAPI.isCloudEventListenerRunning 字段号=7、类型=bool。
func TestQA3v_ResolverCloudAPIDescriptor(t *testing.T) {
	r, err := loadResolver()
	if err != nil {
		t.Fatalf("loadResolver 失败（proto 字段名/号写错会在此暴露）: %v", err)
	}
	out := r.mustMsg("CloudAPIList")
	f := out.Fields().ByName("apis")
	if f == nil || !f.IsList() || f.Kind() != protoreflect.MessageKind {
		t.Fatalf("CloudAPIList.apis 应为 repeated message，实际 %v", f)
	}
	lf := r.mustMsg("CloudAPI").Fields().ByName("isCloudEventListenerRunning")
	if lf == nil {
		t.Fatal("CloudAPI 缺少 isCloudEventListenerRunning 字段")
	}
	if int(lf.Number()) != 7 || lf.Kind() != protoreflect.BoolKind {
		t.Fatalf("isCloudEventListenerRunning 应为 bool 且字段号 7，实际 number=%d kind=%s", lf.Number(), lf.Kind())
	}
}

// ==================== A. 未启动原因日志：翻转重记 + 无 Token 泄露 ====================

// A-1: 未启动→启动→再次未启动时，必须重新记一条（lastReason 被复位）。
func TestQA3v_SupervisorReasonFlipRelogs(t *testing.T) {
	c := Config{EnablePush: false, Address: "127.0.0.1:19798", Token: "x"}
	line1, last := pushSupervisorLogReason(false, c, "")
	if line1 == "" {
		t.Fatal("首次未启动必须记一条")
	}
	line2, last := pushSupervisorLogReason(true, Config{EnablePush: true, Address: "127.0.0.1:19798", Token: "x"}, last)
	if line2 != "" || last != "" {
		t.Fatalf("已启动时应静默并复位 lastReason，实际 line=%q last=%q", line2, last)
	}
	if line3, _ := pushSupervisorLogReason(false, c, last); line3 == "" {
		t.Fatal("复位后再次未启动必须重新记一条")
	}
}

// A-2: 诊断行绝不泄露 Token 明文；空白 Token 记为「无」。
func TestQA3v_DisableLogLineNoTokenLeak(t *testing.T) {
	const secret = "S3CR3T-TOKEN-abcdef"
	for _, c := range []Config{
		{EnablePush: false, Address: "1.2.3.4:19798", Token: secret, Tasks: []string{"/a", "/b"}},
		{EnablePush: true, Address: "", Token: secret},
		{EnablePush: true, Address: "1.2.3.4:19798", Token: "   "},
	} {
		line := pushDisableLogLine(c)
		if strings.Contains(line, secret) {
			t.Fatalf("诊断行泄露 Token：%q", line)
		}
		if !strings.Contains(line, "Token=") {
			t.Fatalf("诊断行应含 Token 有/无：%q", line)
		}
	}
	if tokenPresence("") != "无" || tokenPresence(" \t ") != "无" || tokenPresence("abc") != "有" {
		t.Fatal("tokenPresence 判定错误")
	}
}

// ==================== E. 前端：cloudApis 缺失兼容（结构断言） ====================

// E-1: cloudApis 缺失/null/空数组时有空值保护；「最近收到推送」落在 running 分支内。
func TestQA3v_FrontendCloudApisGuard(t *testing.T) {
	// 说明：pageHTML 头部确有 <title>{{.Title}}</title>（Go 模板，位于 <script> 之外，正常）。
	// 只断言内联 <script> 区间内没有游离的 {{（否则 node --check 会失败）。
	jsStart := strings.Index(pageHTML, "<script>")
	jsEnd := strings.LastIndex(pageHTML, "</script>")
	if jsStart < 0 || jsEnd < jsStart {
		t.Fatal("未找到内联 <script> 区间")
	}
	if strings.Contains(pageHTML[jsStart:jsEnd], "{{") {
		t.Fatal("内联 JS 区间出现 {{，会破坏 node --check")
	}
	if !strings.Contains(pageHTML, "lastStatus&&lastStatus.cloudApis&&lastStatus.cloudApis.length") {
		t.Fatal("renderPush 缺少 cloudApis 空值保护")
	}
	if !strings.Contains(pageHTML, "lastStatus.cloudApis.filter(function(a){return a.isCloudEventListenerRunning===false})") {
		t.Fatal("缺少 isCloudEventListenerRunning=false 的筛选逻辑")
	}
	iRun := strings.Index(pageHTML, "if(st==='running'){")
	iLive := strings.Index(pageHTML, "最近收到推送")
	iDenied := strings.Index(pageHTML, "}else if(st==='denied'){")
	if iRun < 0 || iLive < 0 || iDenied < 0 || !(iRun < iLive && iLive < iDenied) {
		t.Fatalf("「最近收到推送」未落在 running 分支内：run=%d live=%d denied=%d", iRun, iLive, iDenied)
	}
}

// ==================== 辅助 ====================

func mustReadGoFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读取 %s: %v", name, err)
	}
	return string(b)
}
