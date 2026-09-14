package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandleIndex_RendersPage 验证页面可渲染、含关键元素，且禁缓存。
// 禁缓存是必要的：页面是内联单文件，若被浏览器缓存，修好的前端逻辑会被旧副本掩盖。
func TestHandleIndex_RendersPage(t *testing.T) {
	rec := httptest.NewRecorder()
	handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("GET / code=%d，期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, m := range []string{
		"NetDrive Sweeper",
		`id="runBtn"`,
		`id="pushState"`,
		`id="pushHint"`,
		`data-tab="config"`,
		`data-tab="run"`,
		`id="saveBtn2"`,
	} {
		if !strings.Contains(body, m) {
			t.Fatalf("页面缺少 %q", m)
		}
	}
	for _, m := range []string{`id="guideCard"`, `id="offlineBox"`, `id="scanBtn"`, `id="cleanBtn"`} {
		if strings.Contains(body, m) {
			t.Fatalf("页面不应再包含 %q", m)
		}
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("Cache-Control=%q，期望含 no-store（防止旧前端被缓存掩盖）", cc)
	}
}

// TestHandleState_IncludesPushAndScanTime 验证 /api/state 同时输出 push 与 lastScanAt。
func TestHandleState_IncludesPushAndScanTime(t *testing.T) {
	rec := httptest.NewRecorder()
	handleState(rec, httptest.NewRequest("GET", "/api/state", nil))
	body := rec.Body.String()
	for _, m := range []string{`"config"`, `"status"`, `"push"`, `"lastScanAt"`} {
		if !strings.Contains(body, m) {
			t.Fatalf("/api/state 缺少字段 %q，body=%s", m, body)
		}
	}
}

// TestHandleSave_PreservesConfigVersion 验证保存后 config_version 不被前端请求体清零，
// 并如实落盘——否则下次启动会重复执行迁移。
func TestHandleSave_PreservesConfigVersion(t *testing.T) {
	dir := t.TempDir()
	oldCfgPath, oldRecPath, oldLogPath := configPath, recordsPath, logPath
	stateMu.Lock()
	oldCfg := cfg
	stateMu.Unlock()
	configPath = filepath.Join(dir, "config.json")
	recordsPath = filepath.Join(dir, "records.jsonl")
	logPath = filepath.Join(dir, "clean.log")
	defer func() {
		configPath, recordsPath, logPath = oldCfgPath, oldRecPath, oldLogPath
		stateMu.Lock()
		cfg = oldCfg
		stateMu.Unlock()
	}()

	// 前端不发送 config_version（它不在表单里），合并解码必须保留现值。
	req := httptest.NewRequest("POST", "/api/save", strings.NewReader(`{"address":"127.0.0.1:19798","token":"tok","tasks":["/电影"]}`))
	rec := httptest.NewRecorder()
	handleSave(rec, req)
	if rec.Code != 200 {
		t.Fatalf("save code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := currentConfig().ConfigVersion; got != currentConfigVersion {
		t.Fatalf("保存后 config_version=%d，期望 %d", got, currentConfigVersion)
	}
	b, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读回配置失败: %v", err)
	}
	if !strings.Contains(string(b), `"config_version": 1`) {
		t.Fatalf("落盘配置缺少 config_version=1：%s", string(b))
	}
}

// TestMustLoadConfig_MigratesLegacyCooldown 复现用户真实场景：磁盘上的旧 config.json
// （file_cooldown_hours=6，无 config_version）启动时应被一次性迁移为 0 并落盘，
// 且二次加载幂等。这正是「离线下载完成却什么都不删」的隐性原因之一。
func TestMustLoadConfig_MigratesLegacyCooldown(t *testing.T) {
	dir := t.TempDir()
	oldCfgPath, oldRecPath, oldLogPath := configPath, recordsPath, logPath
	stateMu.Lock()
	oldCfg := cfg
	stateMu.Unlock()
	configPath = filepath.Join(dir, "config.json")
	recordsPath = filepath.Join(dir, "records.jsonl")
	logPath = filepath.Join(dir, "clean.log")
	defer func() {
		configPath, recordsPath, logPath = oldCfgPath, oldRecPath, oldLogPath
		stateMu.Lock()
		cfg = oldCfg
		stateMu.Unlock()
	}()

	legacy := `{"address":"127.0.0.1:19798","token":"tok","file_cooldown_hours":6,"enable_push":true,"tasks":["/电影"]}`
	if err := os.WriteFile(configPath, []byte(legacy), 0600); err != nil {
		t.Fatalf("写旧配置失败: %v", err)
	}
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	c := currentConfig()
	if c.FileCooldownHours != 0 {
		t.Fatalf("旧默认 6h 冷却应被迁移为 0，实际 %d", c.FileCooldownHours)
	}
	if c.ConfigVersion != currentConfigVersion {
		t.Fatalf("迁移后 config_version=%d，期望 %d", c.ConfigVersion, currentConfigVersion)
	}
	if len(c.Tasks) != 1 || c.Tasks[0] != "/电影" {
		t.Fatalf("迁移不应丢失既有字段，tasks=%v", c.Tasks)
	}
	b, _ := os.ReadFile(configPath)
	if !strings.Contains(string(b), `"config_version": 1`) || !strings.Contains(string(b), `"file_cooldown_hours": 0`) {
		t.Fatalf("迁移结果未落盘：%s", string(b))
	}
	// 幂等：把值改回 6 再加载，因版本已是 1 而不再迁移。
	if err := os.WriteFile(configPath, []byte(`{"config_version":1,"file_cooldown_hours":6,"token":"tok"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := mustLoadConfig(); err != nil {
		t.Fatal(err)
	}
	if got := currentConfig().FileCooldownHours; got != 6 {
		t.Fatalf("已迁移版本不应再次改写用户值，实际 %d", got)
	}
}
