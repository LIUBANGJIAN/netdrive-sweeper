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

// TestWebStaticMarkers_PushStatusSingleSourceOfTruth 守护「事件驱动状态以后端为唯一可信来源」
// 这一修复不变量（对应问题 1）。
// 历史做法：前端用缓存的 lastToken.allowPushMessage 自行推断并常驻告警——token 状态一陈旧
// 就误报「缺少 allow_push_message」，甚至与徽章显示自相矛盾。
// 新做法：后端 /api/push 输出真实订阅状态（running/denied/config_missing/error），前端只呈现。
func TestWebStaticMarkers_PushStatusSingleSourceOfTruth(t *testing.T) {
	mustContain := map[string]string{
		"顶部推送状态元素":         `id="pushState"`,
		"规则区提示容器":          `id="pushHint"`,
		"renderPush 定义":    `function renderPush(p){`,
		"数据来自后端":           `renderPush(j.push)`,
		"轮询推送状态接口":         `/api/push?_=`,
		"Gap A 禁用并记录原文案":   `btn.dataset.orig=btn.textContent;btn.disabled=true;btn.textContent='测试中…'`,
		"Gap A finally 恢复": `btn.disabled=false;if(btn.dataset.orig){btn.textContent=btn.dataset.orig;delete btn.dataset.orig}`,
	}
	for name, m := range mustContain {
		if !strings.Contains(pageHTML, m) {
			t.Fatalf("%s：pageHTML 缺少标记 %q", name, m)
		}
	}
	// 反回归：不得再用前端缓存的 token 权限去判定推送告警（问题 1 的根因）。
	if strings.Contains(pageHTML, "!lastToken.allowPushMessage") {
		t.Fatal("前端不得再以前端 token 缓存的 allowPushMessage 判定推送告警；应改由后端 /api/push 提供真实状态")
	}
}

// TestWebStaticMarkers_ConsolidatedUI 守护二次精简后的形态：
// 两页式（① 连接·目录·规则 / ② 运行日志）、主操作「手动清理」收敛到运行日志卡片标题栏、
// 保存入口收敛到配置页（saveBtn2）、日志是唯一结果视图；扫描结果表与清理记录已删除。
func TestWebStaticMarkers_ConsolidatedUI(t *testing.T) {
	mustContain := []string{
		`data-tab="config"`,
		`data-tab="logs"`,
		`id="runBtn"`,
		`id="saveBtn2"`,
		`id="logsBox"`,
		`id="lastRunMeta"`,
		`function doRun(){`,
		`手动清理`, // 主操作按钮已更名，且只出现在「② 运行日志」卡片标题栏
		`列目录`,  // 权限徽章中文化（原 list / delete / perm_delete / push_message）
		`回收站删除`,
		`永久删除`,
		`消息推送`,
	}
	for _, m := range mustContain {
		if !strings.Contains(pageHTML, m) {
			t.Fatalf("pageHTML 缺少标记 %q", m)
		}
	}
	mustNotContain := map[string]string{
		"旧按钮文案-手动扫描": `手动扫描`,
		"旧术语-扫描预览":  `扫描预览`,
		"旧术语-执行清理":  `执行清理`,
		"顶栏旧保存按钮": `id="saveBtn"`,
		"旧页签-run": `id="tab-run"`,
		"旧执行摘要":   `id="runMeta"`,
		"旧结果时间":   `id="resTime"`,
		"旧扫描结果表":  `id="resultTbl"`,
		"旧清理记录面板": `id="recPanel"`,
		"旧统计-检查":  `id="statChecked"`,
		"旧统计-命中":  `id="statMatched"`,
		"旧统计-删除":  `id="statDeleted"`,
		"旧统计-跳过":  `id="statSkipped"`,
		"首次配置引导卡": `id="guideCard"`,
		"引导步进器":   `id="stepper"`,
		"离线任务状态表": `id="offlineBox"`,
		"离线任务渲染":  `function renderOffline(`,
		"旧扫描预览按钮": `id="scanBtn"`,
		"旧执行清理按钮": `id="cleanBtn"`,
		"旧页签-连接":  `data-tab="conn"`,
		"旧页签-规则":  `data-tab="rules"`,
	}
	for name, m := range mustNotContain {
		if strings.Contains(pageHTML, m) {
			t.Fatalf("%s：pageHTML 不应再包含 %q", name, m)
		}
	}
}
