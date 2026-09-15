package main

// eventdriven_diag_test.go —— 任务3「事件驱动可诊断性 + 健壮性」回归测试。
// 覆盖四类断言：
//	(a) PushEvent / protobuf 未知字段解析在「有/无 fileSystemChange、字段缺失、超长/非法字节」下
//	    都不 panic 且安全降级；
//	(b) pushSupervisor 的「未启动原因」变化时才记日志、且不重复刷屏，Token 明文绝不进日志；
//	(c) 重连退避序列单调不减且封顶，抖动有界；
//	(d) GetAllCloudApis 解析在 isCloudEventListenerRunning=false 时产告警，且 key 仅在结果变化时改变。
// 另附 P1「订阅存活可见性」的最小证据（任意类型推送计数 + 最近路径）。
// 所有断言只依赖纯函数/内存结构，不依赖真实 CD2 连接。

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// ---------- protobuf wire 构造小工具（仅供测试） ----------

func pbTag(num, wire int) byte { return byte(num<<3 | wire) }

// pbVarintField 生成一个 varint（wire type 0）字段。
func pbVarintField(num int, v uint64) []byte {
	out := []byte{pbTag(num, 0)}
	for v >= 0x80 {
		out = append(out, byte(v&0x7f)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

// pbBytesField 生成一个 length-delimited（wire type 2）字段。
func pbBytesField(num int, b []byte) []byte {
	out := []byte{pbTag(num, 2)}
	l := uint64(len(b))
	for l >= 0x80 {
		out = append(out, byte(l&0x7f)|0x80)
		l >>= 7
	}
	out = append(out, byte(l))
	return append(out, b...)
}

// ==================== (a) 未知字段解析：不 panic + 安全降级 ====================

// a1: decodeVarint 的边界——截断/空串返回 (0,0)，正常串正确。
func TestDiag_DecodeVarintEdges(t *testing.T) {
	cases := []struct {
		in   []byte
		want uint64
		n    int
	}{
		{nil, 0, 0},
		{[]byte{}, 0, 0},
		{[]byte{0x80}, 0, 0},         // 只有续接位，无结尾
		{[]byte{0x05}, 5, 1},         // 单字节
		{[]byte{0xAC, 0x02}, 300, 2}, // 两字节
		{[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}, 0, 0}, // 溢出（>10 字节）
	}
	for i, c := range cases {
		got, n := decodeVarint(c.in)
		if got != c.want || n != c.n {
			t.Fatalf("case %d: decodeVarint(%v) = (%d,%d)，期望 (%d,%d)", i, c.in, got, n, c.want, c.n)
		}
	}
}

// a2: printableText 对非法 UTF-8 / 控制字符返回空，正常文本原样返回。
func TestDiag_PrintableTextEdges(t *testing.T) {
	if s := printableText(nil); s != "" {
		t.Fatalf("nil 应得空串，实际 %q", s)
	}
	if s := printableText([]byte{0xff, 0xfe}); s != "" {
		t.Fatalf("非法 UTF-8 应得空串，实际 %q", s)
	}
	if s := printableText([]byte{'a', 0x01}); s != "" {
		t.Fatalf("含控制字符应得空串，实际 %q", s)
	}
	if s := printableText([]byte("media/a.mkv")); s != "media/a.mkv" {
		t.Fatalf("正常文本应原样返回，实际 %q", s)
	}
}

// a3: parseRawFields 在截断/越界/非法 wire type 下只返回已解析前缀，绝不 panic。
func TestDiag_ParseRawFieldsNeverPanics(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		{0x08},        // tag 声明 wire0，但无 varint 值
		{0x08, 0x80},  // 未终止的 varint
		{0x2a, 0x7f},  // tag=field5 wire2，声明长度 127 但无数据
		{pbTag(5, 3)}, // 非法 wire type 3（group）
		{pbTag(5, 7)}, // 非法 wire type 7
		{pbTag(5, 1)}, // 64-bit 字段但只有 tag
		{pbTag(5, 5)}, // 32-bit 字段但只有 tag
		{0x00, 0x00},  // num=0 非法
		append(pbBytesField(2, []byte("ok")), 0x08), // 正常字段后跟截断
	}
	for i, b := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("case %d: parseRawFields(%v) panic: %v", i, b, r)
				}
			}()
			_ = parseRawFields(b)
		}()
	}
}

// a4: buildFileSystemChangeEvent 无未知字段时安全降级（空路径、OK=false、Raw=top{}）。
func TestDiag_BuildFSC_EmptyUnknown(t *testing.T) {
	ev := buildFileSystemChangeEvent(fileSystemChangeType, nil)
	if ev.Type != fileSystemChangeType {
		t.Fatalf("Type 应保留 messageType，实际 %d", ev.Type)
	}
	if ev.Path != "" || ev.ChangeOK || ev.IsDirOK {
		t.Fatalf("空负载应全部降级为空/未置位，实际 %+v", ev)
	}
	if ev.Raw != "top{}" {
		t.Fatalf("空负载 Raw 应为 \"top{}\"，实际 %q", ev.Raw)
	}
}

// a5: 从 field5(wire2) 的嵌套消息里尽力提取路径。
// 官方未给 FileSystemChange 字段号，故这里只断言「像路径的字符串会被挑出」。
func TestDiag_BuildFSC_ExtractsPath(t *testing.T) {
	inner := pbBytesField(2, []byte("/media/Movies/a.mkv"))
	top := pbBytesField(5, inner) // fileSystemChange 落 unknown（字段号 5）→ 嵌套内容
	ev := buildFileSystemChangeEvent(fileSystemChangeType, top)
	if ev.Path != "/media/Movies/a.mkv" {
		t.Fatalf("应从嵌套里提取路径，实际 %q（Raw=%s）", ev.Path, ev.Raw)
	}
}

// a6: 从嵌套 varint 里尽力推断 changeType / isDirectory，并置 OK 标志。
func TestDiag_BuildFSC_GuessesChangeAndDir(t *testing.T) {
	inner := append(pbVarintField(1, 2), pbVarintField(3, 1)...)
	top := pbBytesField(5, inner)
	ev := buildFileSystemChangeEvent(fileSystemChangeType, top)
	if !ev.ChangeOK || ev.ChangeType != 2 {
		t.Fatalf("应推断 changeType=2 且 OK，实际 %+v", ev)
	}
	if !ev.IsDirOK || !ev.IsDir {
		t.Fatalf("应推断 isDir=true 且 OK，实际 %+v", ev)
	}
}

// a7: 病态负载（截断/非法 wire/超长）下 buildFileSystemChangeEvent 不 panic 且安全降级。
func TestDiag_BuildFSC_MalformedNoPanic(t *testing.T) {
	cases := [][]byte{
		{pbTag(5, 2)},       // field5 wire2 但缺长度
		{pbTag(5, 2), 0x7f}, // 声明长度 127 但无数据
		{pbTag(5, 3)},       // 非法 wire
		append(pbBytesField(5, []byte{0x80}), 0x80), // 内层 varint 截断
	}
	for i, b := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("case %d: buildFileSystemChangeEvent panic: %v", i, r)
				}
			}()
			ev := buildFileSystemChangeEvent(fileSystemChangeType, b)
			if ev.Type != fileSystemChangeType {
				t.Fatalf("case %d: Type 应保留，实际 %d", i, ev.Type)
			}
			// 病理负载只允许安全降级：不得凭空造出「可信」值。
			if ev.Path != "" && !strings.Contains(ev.Path, "/") {
				t.Fatalf("case %d: 有病负载下不应产生可疑路径 %q", i, ev.Path)
			}
		}()
	}
}

// a8: 随机字节 fuzz——解析器必须对任意输入都不 panic（安全降级的核心保证）。
func TestDiag_FSCAndRaw_FuzzNoPanic(t *testing.T) {
	rnd := rand.New(rand.NewSource(20240607))
	for i := 0; i < 20000; i++ {
		n := rnd.Intn(48)
		b := make([]byte, n)
		for j := range b {
			b[j] = byte(rnd.Intn(256))
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("iter=%d 输入 %v 触发 panic: %v", i, b, r)
				}
			}()
			_ = parseRawFields(b)
			ev := buildFileSystemChangeEvent(fileSystemChangeType, b)
			_ = ev.Raw
		}()
	}
}

// ==================== (b) pushSupervisor「未启动原因」仅在变化时记日志 ====================

// b1: want=true（已启动）时不记日志并复位 lastReason。
func TestDiag_SupervisorReason_WantTrueResets(t *testing.T) {
	c := Config{EnablePush: true, Address: "127.0.0.1:19798", Token: "x"}
	line, last := pushSupervisorLogReason(true, c, "some-old-reason")
	if line != "" {
		t.Fatalf("已启动时不应记日志，实际 %q", line)
	}
	if last != "" {
		t.Fatalf("已启动时应复位 lastReason，实际 %q", last)
	}
}

// b2: 首次巡检（lastReason 为空）必记一条完整诊断行。
func TestDiag_SupervisorReason_FirstPatrolAlwaysLogs(t *testing.T) {
	c := Config{EnablePush: false, Address: "127.0.0.1:19798", Token: "x"}
	line, last := pushSupervisorLogReason(false, c, "")
	if line == "" {
		t.Fatal("首次巡检（lastReason 为空）必须记一条诊断行")
	}
	if !strings.Contains(line, "事件驱动未启动") {
		t.Fatalf("诊断行应含「事件驱动未启动」，实际 %q", line)
	}
	if last != pushDisabledReasonKey(c) {
		t.Fatalf("应把当前状态签名写入 lastReason，实际 %q", last)
	}
}

// b3: 原因不变 → 不重复记（不刷屏）；原因变化 → 重新记一条。
func TestDiag_SupervisorReason_OnlyLogsOnChange(t *testing.T) {
	c1 := Config{EnablePush: false, Address: "127.0.0.1:19798", Token: "x"}
	// 首次记录。
	_, last := pushSupervisorLogReason(false, c1, "")
	// 同一原因连续多次：必须保持静默。
	for i := 0; i < 5; i++ {
		line, next := pushSupervisorLogReason(false, c1, last)
		if line != "" {
			t.Fatalf("原因不变时不应重复记日志（第 %d 次巡检），实际 %q", i, line)
		}
		last = next
	}
	// 原因变化（地址清空 → 另一条 reason）：必须重新记。
	c2 := Config{EnablePush: true, Address: "", Token: ""}
	line, last2 := pushSupervisorLogReason(false, c2, last)
	if line == "" {
		t.Fatal("原因变化时应重新记一条诊断行")
	}
	if last2 == last {
		t.Fatalf("原因变化后 lastReason 应更新，仍为 %q", last2)
	}
}

// b4: 诊断行绝不回显 Token 明文，只打「有/无」；并含地址/启用/目录数。
func TestDiag_DisableLogLine_NoPlainToken(t *testing.T) {
	const secret = "SUPER-SECRET-TOKEN-12345"
	c := Config{EnablePush: false, Token: secret, Address: "127.0.0.1:19798", Tasks: []string{"/a", "/b/"}}
	line := pushDisableLogLine(c)
	if strings.Contains(line, secret) {
		t.Fatalf("诊断行泄露 Token 明文：%q", line)
	}
	if !strings.Contains(line, "Token=有") {
		t.Fatalf("诊断行应含 Token=有，实际 %q", line)
	}
	if !strings.Contains(line, "清理目录=2 个") {
		t.Fatalf("诊断行应含清理目录数 2，实际 %q", line)
	}
	if !strings.Contains(line, "启用=false") {
		t.Fatalf("诊断行应含启用=false，实际 %q", line)
	}
	if tokenPresence("") != "无" || tokenPresence("   ") != "无" || tokenPresence("x") != "有" {
		t.Fatal("tokenPresence 有/无判定错误")
	}
}

// b5: 四种「未启动原因」文本必须两两互不相同，使日志能唯一区分失败模式。
func TestDiag_DisabledReasonsFourDistinct(t *testing.T) {
	reasons := map[string]string{
		"未启用":           pushDisabledReason(Config{EnablePush: false, Address: "1.2.3.4:19798", Token: "x"}),
		"地址缺失":          pushDisabledReason(Config{EnablePush: true, Address: "", Token: "x"}),
		"Token 缺失":      pushDisabledReason(Config{EnablePush: true, Address: "1.2.3.4:19798", Token: ""}),
		"地址与 Token 皆缺失": pushDisabledReason(Config{EnablePush: true, Address: "", Token: ""}),
	}
	seen := map[string]string{}
	for name, r := range reasons {
		if r == "" {
			t.Fatalf("%s 的原因文本不应为空", name)
		}
		if prev, dup := seen[r]; dup {
			t.Fatalf("原因文本重复：「%s」与「%s」都是 %q（无法区分失败模式）", name, prev, r)
		}
		seen[r] = name
	}
	// 抽查关键文案，避免退化成含糊的通用句。
	if !strings.Contains(reasons["未启用"], "已关闭") {
		t.Fatalf("未启用原因应含「已关闭」，实际 %q", reasons["未启用"])
	}
	if !strings.Contains(reasons["地址缺失"], "地址未配置") || strings.Contains(reasons["地址缺失"], "Token") {
		t.Fatalf("地址缺失原因应为「地址未配置」且不提 Token，实际 %q", reasons["地址缺失"])
	}
	if !strings.Contains(reasons["Token 缺失"], "Token 未配置") || strings.Contains(reasons["Token 缺失"], "地址") {
		t.Fatalf("Token 缺失原因应为「Token 未配置」且不提地址，实际 %q", reasons["Token 缺失"])
	}
	if !strings.Contains(reasons["地址与 Token 皆缺失"], "均未配置") {
		t.Fatalf("两者皆缺失原因应含「均未配置」，实际 %q", reasons["地址与 Token 皆缺失"])
	}
}

// b6: 关键字段发生变化时，即便原因文本相同也必须重新记日志（签名判据的核心价值）。
// 用「已关闭」这一原因（地址/Token 可自由变化）演示「Token 无→有」被捕捉。
func TestDiag_SupervisorReason_FieldChangeRelogs(t *testing.T) {
	noTok := Config{EnablePush: false, Address: "1.2.3.4:19798", Token: ""}
	hasTok := Config{EnablePush: false, Address: "1.2.3.4:19798", Token: "x"}
	if pushDisabledReason(noTok) != pushDisabledReason(hasTok) {
		t.Fatalf("前置条件错误：本用例要求两者原因文本相同，实际 %q vs %q",
			pushDisabledReason(noTok), pushDisabledReason(hasTok))
	}

	line1, last := pushSupervisorLogReason(false, noTok, "")
	if line1 == "" {
		t.Fatal("首次未启动必须记一条")
	}
	// 原因文本不变、但 Token 从「无」→「有」：必须重新记（签名变化）。
	line2, last2 := pushSupervisorLogReason(false, hasTok, last)
	if line2 == "" {
		t.Fatal("Token 无→有时应重新记日志（字段级变化被签名捕捉）")
	}
	if last2 == last {
		t.Fatalf("字段变化后签名应更新，仍为 %q", last2)
	}
	// 稳定后不得再刷屏。
	if again, _ := pushSupervisorLogReason(false, hasTok, last2); again != "" {
		t.Fatalf("状态稳定后不应重复记日志，实际 %q", again)
	}
}

// b7: 「地址为空 ↔ Token 为空」之间切换必须各自重记（此前共用文本导致被吞）。
func TestDiag_SupervisorReason_AddressVsTokenSwitchRelogs(t *testing.T) {
	addrEmpty := Config{EnablePush: true, Address: "", Token: "x"}
	tokenEmpty := Config{EnablePush: true, Address: "1.2.3.4:19798", Token: ""}

	_, last := pushSupervisorLogReason(false, addrEmpty, "")
	line, last2 := pushSupervisorLogReason(false, tokenEmpty, last)
	if line == "" {
		t.Fatal("地址为空 → Token 为空切换时应重新记日志（字段变化被捕捉）")
	}
	if last2 == last {
		t.Fatalf("切换后签名应更新，仍为 %q", last2)
	}
	// 来回切换也必须每次重记（各自对应不同签名）。
	line3, _ := pushSupervisorLogReason(false, addrEmpty, last2)
	if line3 == "" {
		t.Fatal("Token 为空 → 地址为空切回时也应重新记日志")
	}
}

// ==================== (c) 重连退避：单调不减 + 封顶 + 抖动有界 ====================

// c1: reconnectBackoff 序列为 2s→4s→8s→16s→30s 且此后稳定封顶。
func TestDiag_ReconnectBackoffMonotonicAndCapped(t *testing.T) {
	want := []time.Duration{
		2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second,
	}
	for i, w := range want {
		if got := reconnectBackoff(i); got != w {
			t.Fatalf("reconnectBackoff(%d) = %v，期望 %v", i, got, w)
		}
	}
	prev := time.Duration(0)
	for i := 0; i < 40; i++ {
		d := reconnectBackoff(i)
		if d < prev {
			t.Fatalf("退避序列在 attempt=%d 递减：%v → %v", i, prev, d)
		}
		if d > reconnectMax {
			t.Fatalf("退避在 attempt=%d 超过封顶 %v：%v", i, reconnectMax, d)
		}
		prev = d
	}
	if reconnectBackoff(1000) != reconnectMax {
		t.Fatalf("极大 attempt 应封顶 %v，实际 %v", reconnectMax, reconnectBackoff(1000))
	}
}

// c2: withReconnectJitter 抖动有界（∈[0.8d, d]），且 rnd=0 时取 0.8d。
func TestDiag_ReconnectJitterBounded(t *testing.T) {
	const d = 30 * time.Second
	if got := withReconnectJitter(d, 0); got != 24*time.Second {
		t.Fatalf("rnd=0 应得 0.8d=24s，实际 %v", got)
	}
	rnd := rand.New(rand.NewSource(7))
	for i := 0; i < 1000; i++ {
		r := rnd.Float64() // [0,1)
		got := withReconnectJitter(d, r)
		if got < 24*time.Second || got > d {
			t.Fatalf("抖动越界：rnd=%v → %v（应 ∈[24s,30s]）", r, got)
		}
	}
}

// ==================== (d) GetAllCloudApis 解析 + 仅变化时记一次 ====================

// d1: cloudListenerReport——isCloudEventListenerRunning=false 的文案按「是否有仍能收到推送的证据」分级：
// 无证据时产出提示（不再绝对化断言）；有证据时产出「已确证仍能收到」的提示，避免误报。
func TestDiag_CloudListenerReport_GradedByEvidence(t *testing.T) {
	// pushLive=false：非绝对化提示，不再出现吓人的「警告」与「CD2 将不再推送文件变更事件」。
	lines, key := cloudListenerReport([]CloudAPI{{Name: "115", IsCloudEventListenerRunning: false}}, false)
	if len(lines) != 1 {
		t.Fatalf("应产出 1 行，实际 %d", len(lines))
	}
	if !strings.Contains(lines[0], "提示") || !strings.Contains(lines[0], "未运行") {
		t.Fatalf("false 应产提示，实际 %q", lines[0])
	}
	if strings.Contains(lines[0], "警告") {
		t.Fatalf("不应再出现「警告」绝对化措辞，实际 %q", lines[0])
	}
	if strings.Contains(lines[0], "CD2 将不再推送文件变更事件") {
		t.Fatalf("不应再出现「CD2 将不再推送文件变更事件」的绝对化断言，实际 %q", lines[0])
	}
	if key != "115=false;pushLive=false" {
		t.Fatalf("key 应为 115=false;pushLive=false，实际 %q", key)
	}

	// pushLive=true：降级为「已确证仍能收到」的提示。
	linesLive, keyLive := cloudListenerReport([]CloudAPI{{Name: "115", IsCloudEventListenerRunning: false}}, true)
	if len(linesLive) != 1 || !strings.Contains(linesLive[0], "已确证仍能收到") {
		t.Fatalf("pushLive=true 应产「已确证仍能收到」提示，实际 %v", linesLive)
	}
	if !strings.Contains(keyLive, "pushLive=true") {
		t.Fatalf("key 应含 pushLive=true，实际 %q", keyLive)
	}
}

// d2: cloudListenerReport——key 稳定：相同结果 key 不变（→ 调用方仅在变化时记一次）；不同结果 key 变；
// 且「证据位 pushLive」翻转时 key 也必须变。
func TestDiag_CloudListenerReport_KeyChangesOnlyOnResultChange(t *testing.T) {
	_, k1 := cloudListenerReport([]CloudAPI{{Name: "115", IsCloudEventListenerRunning: false}}, false)
	_, k1again := cloudListenerReport([]CloudAPI{{Name: "115", IsCloudEventListenerRunning: false}}, false)
	if k1 != k1again {
		t.Fatalf("相同结果 key 必须稳定：%q vs %q", k1, k1again)
	}
	_, k2 := cloudListenerReport([]CloudAPI{{Name: "115", IsCloudEventListenerRunning: true}}, false)
	if k2 == k1 {
		t.Fatalf("结果变化后 key 必须改变，仍为 %q", k1)
	}
	if !strings.Contains(k2, "true") {
		t.Fatalf("true 结果 key 应含 true，实际 %q", k2)
	}
	// 仅证据位翻转（同盘同监听器状态）也必须改变 key。
	_, kLive := cloudListenerReport([]CloudAPI{{Name: "115", IsCloudEventListenerRunning: false}}, true)
	if kLive == k1 {
		t.Fatalf("pushLive 翻转后 key 必须改变，仍为 %q", k1)
	}
	// 多盘：key 拼接且顺序稳定（末尾附 pushLive 证据位）。
	_, kmulti := cloudListenerReport([]CloudAPI{
		{Name: "a", IsCloudEventListenerRunning: true},
		{Name: "b", IsCloudEventListenerRunning: false},
	}, false)
	if kmulti != "a=true;b=false;pushLive=false" {
		t.Fatalf("多盘 key 应为 a=true;b=false;pushLive=false，实际 %q", kmulti)
	}
}

// d3: parseCloudAPIs 从真实 CloudAPIList 动态消息解析出 name / isCloudEventListenerRunning。
func TestDiag_ParseCloudAPIs_FromDynamicMessage(t *testing.T) {
	r, err := loadResolver()
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}
	out := dynamicpb.NewMessage(r.mustMsg("CloudAPIList"))
	lf := out.Descriptor().Fields().ByName("apis")
	list := out.Mutable(lf).List()

	add := func(name string, running bool) {
		api := dynamicpb.NewMessage(r.mustMsg("CloudAPI"))
		fd := api.Descriptor().Fields()
		api.Set(fd.ByName("name"), protoreflect.ValueOfString(name))
		api.Set(fd.ByName("isCloudEventListenerRunning"), protoreflect.ValueOfBool(running))
		list.Append(protoreflect.ValueOfMessage(api))
	}
	add("115", false)
	add("aliyun", true)

	got := parseCloudAPIs(out)
	if len(got) != 2 {
		t.Fatalf("应解析出 2 个云盘，实际 %d", len(got))
	}
	if got[0].Name != "115" || got[0].IsCloudEventListenerRunning {
		t.Fatalf("第 1 个云盘解析错误：%+v", got[0])
	}
	if got[1].Name != "aliyun" || !got[1].IsCloudEventListenerRunning {
		t.Fatalf("第 2 个云盘解析错误：%+v", got[1])
	}

	// 关键链路：解析结果喂给 cloudListenerReport，必须对 false 的那个产提示（非绝对化告警）。
	lines, key := cloudListenerReport(got, false)
	if !strings.Contains(strings.Join(lines, "\n"), "提示") {
		t.Fatalf("含 false 的解析结果应产提示，实际 %v", lines)
	}
	if !strings.Contains(key, "115=false") {
		t.Fatalf("key 应含 115=false，实际 %q", key)
	}
}

// d4: parseCloudAPIs 对「无 apis 字段」的消息类型安全降级（返回 nil，不 panic）。
func TestDiag_ParseCloudAPIs_MissingFieldSafe(t *testing.T) {
	r, err := loadResolver()
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}
	// CloudAPI 自身没有 apis 字段 → parseCloudAPIs 应安全返回 nil。
	out := dynamicpb.NewMessage(r.mustMsg("CloudAPI"))
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("parseCloudAPIs 对无 apis 字段的消息 panic: %v", rec)
			}
		}()
		if got := parseCloudAPIs(out); got != nil {
			t.Fatalf("无 apis 字段时应返回 nil，实际 %v", got)
		}
	}()
	// 空 CloudAPIList（apis 为空）→ 长度为 0，不 panic。
	empty := dynamicpb.NewMessage(r.mustMsg("CloudAPIList"))
	if got := parseCloudAPIs(empty); len(got) != 0 {
		t.Fatalf("空列表应解析为 0 个，实际 %d", len(got))
	}
}

// ==================== P1 订阅存活可见性（任意类型推送计数 + 最近路径） ====================

// p1: markPushMessage 累计各类型计数并刷新 LastMessageAt；pushSnapshot 返回深拷贝。
func TestDiag_PushSnapshotCountsAndDeepCopy(t *testing.T) {
	pushMu.Lock()
	oldStat := pushStat
	pushStat = PushRuntime{State: "off", Detail: "未启用"}
	pushMu.Unlock()
	defer func() { pushMu.Lock(); pushStat = oldStat; pushMu.Unlock() }()

	markPushMessage(2) // UPDATE_STATUS
	markPushMessage(2)
	markPushMessage(4) // FILE_SYSTEM_CHANGE

	snap := pushSnapshot()
	if snap.TypeCounts[2] != 2 || snap.TypeCounts[4] != 1 {
		t.Fatalf("类型计数错误：%+v", snap.TypeCounts)
	}
	if snap.LastMessageAt == "" {
		t.Fatal("markPushMessage 应刷新 LastMessageAt")
	}
	// 深拷贝：改动快照不得污染内部状态。
	snap.TypeCounts[2] = 999
	if pushSnapshot().TypeCounts[2] != 2 {
		t.Fatal("pushSnapshot 未做深拷贝：外部改动污染了内部 map")
	}
}

// p2: markPushEventPath 记录最近路径；空串被忽略（不清空既有值）。
func TestDiag_MarkPushEventPath(t *testing.T) {
	pushMu.Lock()
	oldStat := pushStat
	pushStat = PushRuntime{}
	pushMu.Unlock()
	defer func() { pushMu.Lock(); pushStat = oldStat; pushMu.Unlock() }()

	markPushEventPath("")
	if pushSnapshot().LastEventPath != "" {
		t.Fatal("空路径应被忽略")
	}
	markPushEventPath("/media/a.mkv")
	if pushSnapshot().LastEventPath != "/media/a.mkv" {
		t.Fatalf("应记录最近路径，实际 %q", pushSnapshot().LastEventPath)
	}
	markPushEventPath("")
	if pushSnapshot().LastEventPath != "/media/a.mkv" {
		t.Fatal("空路径不应清空既有路径")
	}
}
