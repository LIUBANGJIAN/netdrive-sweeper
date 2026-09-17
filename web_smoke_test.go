package main

import (
	"fmt"
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
		`id="eventScanMinInterval"`, // F2 扫描冷却的 UI 输入（存量用户可调；见 config 迁移 v1→v2）
		`id="eventFallbackScan"`,    // 事件静默兜底扫描的 UI 输入（见 config 迁移 v2→v3）
		`id="lastRunMeta"`,
		`id="logsBox"`,
		`data-tab="config"`,
		`data-tab="logs"`,
		`id="saveBtn2"`,
		`手动清理`, // 主操作按钮已由「手动扫描」更名为「手动清理」
		`列目录`,  // 权限徽章中文化
		`回收站删除`,
		`永久删除`,
		`消息推送`,
	} {
		if !strings.Contains(body, m) {
			t.Fatalf("页面缺少 %q", m)
		}
	}
	// 主操作按钮必须落在「② 运行日志」页签之后（即运行日志卡片标题栏内），
	// 不再常驻顶栏、不在配置页出现。
	if iRun, iLogs := strings.Index(body, `id="runBtn"`), strings.Index(body, `data-tab="logs"`); iRun < iLogs {
		t.Fatalf("「手动清理」按钮应位于运行日志卡片标题栏内（runBtn 索引 %d 应大于 tab-logs 索引 %d）", iRun, iLogs)
	}
	for _, m := range []string{`id="saveBtn"`, `id="resultTbl"`, `id="recPanel"`, `id="statChecked"`, `id="guideCard"`, `id="offlineBox"`, `id="scanBtn"`, `id="cleanBtn"`, `手动扫描`} {
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
	// 等 handleSave 的「保存后自检」goroutine 收敛，避免其在 logPath 恢复后污染真实日志（LIFO：先于上面恢复执行）。
	defer selfCheckWG.Wait()

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
	if want := fmt.Sprintf(`"config_version": %d`, currentConfigVersion); !strings.Contains(string(b), want) {
		t.Fatalf("落盘配置缺少 %s：%s", want, string(b))
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
	if !strings.Contains(string(b), fmt.Sprintf(`"config_version": %d`, currentConfigVersion)) || !strings.Contains(string(b), `"file_cooldown_hours": 0`) {
		t.Fatalf("迁移结果未落盘：%s", string(b))
	}
	// 防回归：v1 配置同样会进入 migrateConfig；靠 v0→v1 步的 `ConfigVersion < 1` 守卫跳过该步，
	// 用户显式的 6 才得以保留（若缺此守卫，版本 bump 到 2 后 v1 的 6 会被误改写成 0）。
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

// TestMustLoadConfig_MigratesEventScanInterval 复现存量用户场景：磁盘上的旧 config.json
// （无 config_version 且不含 event_scan_min_interval_minutes）升级启动时，应被一次性迁移为
// 默认 5 分钟并落盘——否则 F2 扫描冷却对「报障的存量用户」形同不存在（反序列化得 0=关闭）。
// 并断言：版本已是最新（2）且用户显式设 0（关闭冷却）的配置不得被改写（尊重「关闭冷却」意图）。
func TestMustLoadConfig_MigratesEventScanInterval(t *testing.T) {
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

	// 旧版本配置：无 config_version（→0）且不含新字段（→0）。启动后应迁移为 5 并落盘。
	legacy := `{"address":"127.0.0.1:19798","token":"tok","enable_push":true,"tasks":["/电影"]}`
	if err := os.WriteFile(configPath, []byte(legacy), 0600); err != nil {
		t.Fatalf("写旧配置失败: %v", err)
	}
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	c := currentConfig()
	if c.EventScanMinIntervalMinutes != 5 {
		t.Fatalf("旧配置升级后事件扫描最小间隔应为 5，实际 %d", c.EventScanMinIntervalMinutes)
	}
	if c.ConfigVersion != currentConfigVersion {
		t.Fatalf("迁移后 config_version=%d，期望 %d", c.ConfigVersion, currentConfigVersion)
	}
	if len(c.Tasks) != 1 || c.Tasks[0] != "/电影" {
		t.Fatalf("迁移不应丢失既有字段，tasks=%v", c.Tasks)
	}
	b, _ := os.ReadFile(configPath)
	if want := fmt.Sprintf(`"event_scan_min_interval_minutes": %d`, 5); !strings.Contains(string(b), want) {
		t.Fatalf("迁移结果未落盘（应含 %s）：%s", want, string(b))
	}

	// 尊重用户意图：版本已是最新且显式设 0 → 不得改写成 5。
	cur := fmt.Sprintf(`{"config_version":%d,"event_scan_min_interval_minutes":0,"token":"tok"}`, currentConfigVersion)
	if err := os.WriteFile(configPath, []byte(cur), 0600); err != nil {
		t.Fatal(err)
	}
	if err := mustLoadConfig(); err != nil {
		t.Fatal(err)
	}
	if got := currentConfig().EventScanMinIntervalMinutes; got != 0 {
		t.Fatalf("版本已是最新时不得改写用户显式设置的 0（关闭冷却），实际 %d", got)
	}
}
