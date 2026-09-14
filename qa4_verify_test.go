package main

// qa4_verify_test.go —— 第二层独立验证（针对收尾提交 f8c7061：诊断文案拆分 + 测试不再污染 data/clean.log）。
//
// 独立于实现者的 eventdriven_diag_test.go：从「字段级变化的可观测性」与「签名不泄露 Token」两个
// 对抗性角度，直接证明修复可用，而非仅仅确认函数存在。全部用例复用既有 withTempPaths，杜绝污染真实日志。

import (
	"strings"
	"testing"
)

// (a) pushDisabledReason 必须对四种失败模式返回四个两两互不相同的字符串。
func TestQA4_PushDisabledReasonFourDistinct(t *testing.T) {
	defer withTempPaths(t)()

	type tc struct {
		name       string
		cfg        Config
		wantSub    string
		mustNotSub []string
	}
	cases := []tc{
		{"已关闭", Config{EnablePush: false, Address: "1.2.3.4:19798", Token: "x"}, "已关闭", nil},
		{"地址未配置", Config{EnablePush: true, Address: "", Token: "x"}, "地址未配置", []string{"Token"}},
		{"Token未配置", Config{EnablePush: true, Address: "1.2.3.4:19798", Token: ""}, "Token 未配置", []string{"地址"}},
		{"地址与Token均未配置", Config{EnablePush: true, Address: "", Token: ""}, "均未配置", nil},
	}
	seen := map[string]string{}
	for _, c := range cases {
		r := pushDisabledReason(c.cfg)
		if r == "" {
			t.Fatalf("[%s] 原因文本不应为空", c.name)
		}
		if !strings.Contains(r, c.wantSub) {
			t.Fatalf("[%s] 原因文本应含 %q，实际 %q", c.name, c.wantSub, r)
		}
		for _, bad := range c.mustNotSub {
			if strings.Contains(r, bad) {
				t.Fatalf("[%s] 原因文本不应含 %q（无法唯一区分失败模式），实际 %q", c.name, bad, r)
			}
		}
		if prev, dup := seen[r]; dup {
			t.Fatalf("原因文本重复，无法区分：「%s」与「%s」同为 %q", prev, c.name, r)
		}
		seen[r] = c.name
	}
	if len(seen) != 4 {
		t.Fatalf("四种失败模式应产生 4 个互不相同的原因文本，实际 %d 个：%v", len(seen), seen)
	}
	for r, name := range seen {
		t.Logf("失败模式「%s」→ reason=%q", name, r)
	}
}

// (b)-1 「地址空+Token空」→「地址已填+Token空」这类字段级变化必须产生不同签名，并触发重新记日志。
func TestQA4_ReasonKeyFieldChangeRelogs(t *testing.T) {
	defer withTempPaths(t)()

	bothEmpty := Config{EnablePush: true, Address: "", Token: ""}
	addrFilled := Config{EnablePush: true, Address: "1.2.3.4:19798", Token: ""}
	tokenFilled := Config{EnablePush: true, Address: "", Token: "x"}

	kBoth := pushDisabledReasonKey(bothEmpty)
	kAddr := pushDisabledReasonKey(addrFilled)
	kTok := pushDisabledReasonKey(tokenFilled)
	if kBoth == kAddr {
		t.Fatalf("「地址空→已填」签名不应相同：%q", kBoth)
	}
	if kBoth == kTok || kAddr == kTok {
		t.Fatalf("三种状态签名应两两不同：both=%q addr=%q tok=%q", kBoth, kAddr, kTok)
	}
	t.Logf("签名(地址空+Token空)  =%q", kBoth)
	t.Logf("签名(地址已填+Token空)=%q", kAddr)
	t.Logf("签名(地址空+Token已填)=%q", kTok)

	// 通过 supervisor 判据端到端确认「会重新记日志」。
	line1, last := pushSupervisorLogReason(false, bothEmpty, "")
	if line1 == "" {
		t.Fatal("首次未启动必须记一条诊断行")
	}
	if last != kBoth {
		t.Fatalf("首次后 lastReason 应为 bothEmpty 的签名，实际 %q", last)
	}
	line2, last2 := pushSupervisorLogReason(false, addrFilled, last)
	if line2 == "" {
		t.Fatal("「地址空→已填」应重新记日志（字段级变化被签名捕捉）")
	}
	if last2 == last {
		t.Fatalf("字段变化后签名应更新，仍为 %q", last2)
	}
	line3, last3 := pushSupervisorLogReason(false, tokenFilled, last2)
	if line3 == "" {
		t.Fatal("「Token 空→已填」应重新记日志")
	}
	if last3 == last2 {
		t.Fatalf("字段变化后签名应更新，仍为 %q", last3)
	}
	// 反向（地址已填→空）也必须重记。
	if line4, _ := pushSupervisorLogReason(false, addrFilled, last3); line4 == "" {
		t.Fatal("「Token 已填→地址空（反向切换）」也应重新记日志")
	}
}

// (b)-2 签名与诊断行绝不出现 Token 明文；且签名只比较「有/无」，Token 内容变化不改变签名。
func TestQA4_ReasonKeyNoTokenLeak(t *testing.T) {
	defer withTempPaths(t)()

	const secret = "SUPER-SECRET-TOKEN-abcdef-99"
	withTok := Config{EnablePush: true, Address: "1.2.3.4:19798", Token: secret}
	otherTok := Config{EnablePush: true, Address: "1.2.3.4:19798", Token: "another-different-token-value"}

	key := pushDisabledReasonKey(withTok)
	if strings.Contains(key, secret) {
		t.Fatalf("状态签名泄露 Token 明文：%q", key)
	}
	if !strings.Contains(key, "\x00") {
		t.Fatalf("状态签名应以 \\x00 拼接字段，实际 %q", key)
	}
	t.Logf("含密 Token 的签名=%q（仅含 Token 有/无，无明文）", key)
	// 只比较「有/无」：两个不同 Token（均为「有」）签名必须相同，证明签名不依赖 Token 内容。
	if pushDisabledReasonKey(otherTok) != key {
		t.Fatalf("Token 内容不同但均非空时签名不应改变：%q vs %q", pushDisabledReasonKey(otherTok), key)
	}
	// 诊断行同样不得含明文。
	if line := pushDisableLogLine(withTok); strings.Contains(line, secret) {
		t.Fatalf("诊断行泄露 Token 明文：%q", line)
	}
}

// (e) 稳态下 supervisor 对同一「未启动」状态绝不重复刷日志。
func TestQA4_SupervisorSteadyStateNoRepeat(t *testing.T) {
	defer withTempPaths(t)()

	c := Config{EnablePush: true, Address: "", Token: "", Tasks: []string{"/a", "/b"}}
	_, last := pushSupervisorLogReason(false, c, "")
	for i := 0; i < 8; i++ {
		line, next := pushSupervisorLogReason(false, c, last)
		if line != "" {
			t.Fatalf("稳态第 %d 次巡检不应重复记日志，实际 %q", i, line)
		}
		last = next
	}
}
