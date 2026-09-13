package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------- H2: cleanTasks 跳过空白项 ----------

// TestCleanTasks_SkipsBlankEntries 验证空串/空白项被丢弃，而不是被 normalizePath 放大成 "/"。
func TestCleanTasks_SkipsBlankEntries(t *testing.T) {
	got := cleanTasks([]string{"", "  ", "/电影", ""})
	if len(got) != 1 || got[0] != "/电影" {
		t.Fatalf("cleanTasks=%v，期望 [/电影]（空白项应被跳过，不得变成 /）", got)
	}
}

// TestCleanTasks_KeepsExplicitRoot 验证用户显式填写的 "/" 被保留（合法：扫根目录）。
func TestCleanTasks_KeepsExplicitRoot(t *testing.T) {
	got := cleanTasks([]string{"/"})
	if len(got) != 1 || got[0] != "/" {
		t.Fatalf("cleanTasks([\"/\"])=%v，期望 [\"/\"]（不得误伤显式根目录）", got)
	}
}

// TestCleanTasks_BlankOnlyBecomesEmpty 验证纯空白项产出空列表（空目录语义）。
func TestCleanTasks_BlankOnlyBecomesEmpty(t *testing.T) {
	if got := cleanTasks([]string{"", "   ", "\t", "\n"}); len(got) != 0 {
		t.Fatalf("纯空白项应产出空列表，实际 %v", got)
	}
}

// TestCleanTasks_DedupAndNormalize 确认原有去重 + 规范化行为未被破坏。
func TestCleanTasks_DedupAndNormalize(t *testing.T) {
	// "电影/测试" 与 "/电影/测试" 规范化后等价 → 去重为一项。
	got := cleanTasks([]string{"电影/测试", "/电影/测试", "/动画"})
	if len(got) != 2 || got[0] != "/电影/测试" || got[1] != "/动画" {
		t.Fatalf("cleanTasks=%v，期望 [/电影/测试 /动画]", got)
	}
}

// ---------- H1: handleScan / handleClean 空目录前置守卫 ----------

// withTasks 临时改写全局 cfg.Tasks，返回恢复函数。
func withTasks(tasks []string) func() {
	stateMu.Lock()
	old := cfg
	cfg.Tasks = tasks
	stateMu.Unlock()
	return func() {
		stateMu.Lock()
		cfg = old
		stateMu.Unlock()
	}
}

// TestHandleScan_EmptyTasksFriendlyErrorBeforeCD2 验证空目录时 handleScan 直接返回
// 「未配置任何目录」，而不是先连 CD2 报连接错误（T7 / 采纳的开放问题 1）。
func TestHandleScan_EmptyTasksFriendlyErrorBeforeCD2(t *testing.T) {
	defer withTasks([]string{})()
	rec := httptest.NewRecorder()
	handleScan(rec, httptest.NewRequest("GET", "/api/scan", nil))
	if rec.Code != 400 {
		t.Fatalf("空目录 scan code=%d，期望 400 body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode scan error resp: %v body=%s", err, rec.Body.String())
	}
	if payload.OK {
		t.Fatalf("空目录 scan 不应返回 ok=true：%s", rec.Body.String())
	}
	if !strings.Contains(payload.Error, "未配置任何目录") {
		t.Fatalf("错误信息=%q，期望包含「未配置任何目录」", payload.Error)
	}
}

// TestHandleClean_EmptyTasksFriendlyErrorBeforeCD2 同上，验证 handleClean。
func TestHandleClean_EmptyTasksFriendlyErrorBeforeCD2(t *testing.T) {
	defer withTasks([]string{})()
	rec := httptest.NewRecorder()
	handleClean(rec, httptest.NewRequest("POST", "/api/clean", nil))
	if rec.Code != 400 {
		t.Fatalf("空目录 clean code=%d，期望 400 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "未配置任何目录") {
		t.Fatalf("body=%s，期望包含「未配置任何目录」", rec.Body.String())
	}
}

// ---------- Gap A / Gap B: HTML/JS 静态标记回归 ----------

// TestWebStaticMarkers_GapA_GapB 以源码级断言守护设计稿验收项。
// Gap A：测试连接期间按钮 disabled + 文案「测试中…」，并在 finally 恢复；
// Gap B：enablePush 下方常驻橙色告警 + renderPushWarn 定义与三个调用点。
func TestWebStaticMarkers_GapA_GapB(t *testing.T) {
	markers := map[string]string{
		"Gap B 告警元素":            `id="pushWarn"`,
		"Gap B 告警文案":            `当前 Token 缺少 allow_push_message，事件驱动实时清理不会生效`,
		"Gap B 函数定义":            `function renderPushWarn()`,
		"Gap B 可见条件(推送权限)":      `checked('enablePush')&&lastToken&&!lastToken.allowPushMessage`,
		"Gap B 调用点 renderPerms": `renderPushWarn();`, // renderPerms 末尾
		"Gap B 调用点 change":      `el('enablePush').addEventListener('change',renderPushWarn)`,
		"Gap A 禁用并记录原文案":        `btn.dataset.orig=btn.textContent;btn.disabled=true;btn.textContent='测试中…'`,
		"Gap A finally 恢复":      `btn.disabled=false;if(btn.dataset.orig){btn.textContent=btn.dataset.orig;delete btn.dataset.orig}`,
	}
	for name, m := range markers {
		if !strings.Contains(pageHTML, m) {
			t.Fatalf("%s：pageHTML 缺少标记 %q", name, m)
		}
	}
	// fillConfig 末尾也必须调用 renderPushWarn（首个加载即生效）。
	if !strings.Contains(pageHTML, `setDirty(false);renderGuide();updateNextAction();renderPushWarn();`) {
		t.Fatal("Gap B 调用点 fillConfig：缺少 renderPushWarn() 调用")
	}
}
