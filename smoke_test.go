package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// withTempPaths 把全局的配置/记录/日志路径指向 t.TempDir()，
// 并在测试结束后恢复原值（含全局 cfg），避免污染其他用例。
func withTempPaths(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	oldCfg, oldRec, oldLog := configPath, recordsPath, logPath
	oldState := currentConfig()
	configPath = filepath.Join(dir, "config.json")
	recordsPath = filepath.Join(dir, "records.jsonl")
	logPath = filepath.Join(dir, "clean.log")
	return func() {
		configPath, recordsPath, logPath = oldCfg, oldRec, oldLog
		stateMu.Lock()
		cfg = oldState
		stateMu.Unlock()
	}
}

// TestSmoke_FreshConfigHasEmptyTasks 验证新装默认配置不再回填 ["/"]（T7）。
func TestSmoke_FreshConfigHasEmptyTasks(t *testing.T) {
	defer withTempPaths(t)()
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	if got := currentConfig().Tasks; len(got) != 0 {
		t.Fatalf("默认 tasks=%v，期望为空（T7：空目录=不扫描）", got)
	}
}

// TestSmoke_StateEndpointReturnsEmptyTasks 验证 GET /api/state 返回 tasks:[]。
func TestSmoke_StateEndpointReturnsEmptyTasks(t *testing.T) {
	defer withTempPaths(t)()
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	rec := httptest.NewRecorder()
	handleState(rec, httptest.NewRequest("GET", "/api/state", nil))
	if rec.Code != 200 {
		t.Fatalf("handleState code=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	raw, ok := payload.Config["tasks"]
	if !ok {
		t.Fatal("state.config 缺少 tasks 字段")
	}
	var tasks []string
	if err := json.Unmarshal(raw, &tasks); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("state tasks=%v，期望为空数组", tasks)
	}
}

// TestSmoke_SaveMergeDecodePreservesFields 验证 handleSave 以现有配置为基底合并解码：
// 请求体只带一个字段时，其余字段保留现值而非被重置为零值（同类问题见 FIX-A）。
func TestSmoke_SaveMergeDecodePreservesFields(t *testing.T) {
	defer withTempPaths(t)()
	defer selfCheckWG.Wait() // 等 handleSave 的「保存后自检」goroutine 收敛，避免污染真实日志
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	before := currentConfig()

	// 仅提交 tasks，其余一律省略。
	body := `{"tasks":["/电影","/动画"]}`
	rec := httptest.NewRecorder()
	handleSave(rec, httptest.NewRequest("POST", "/api/save", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("handleSave code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK     bool   `json:"ok"`
		Config Config `json:"config"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode save resp: %v", err)
	}
	if !resp.OK {
		t.Fatalf("save not ok: %s", rec.Body.String())
	}
	got := resp.Config
	// 未提交字段必须保留默认值（若为合并解码则非零；若整体覆盖会变成零值）。
	if got.MaxFilesPerRun != before.MaxFilesPerRun {
		t.Fatalf("max_files_per_run=%d，期望保留 %d（合并解码失败）", got.MaxFilesPerRun, before.MaxFilesPerRun)
	}
	if got.MaxTotalBytes != before.MaxTotalBytes {
		t.Fatalf("max_total_bytes=%d，期望保留 %d", got.MaxTotalBytes, before.MaxTotalBytes)
	}
	if got.Burst != before.Burst {
		t.Fatalf("burst=%d，期望保留 %d", got.Burst, before.Burst)
	}
	if strings.TrimSpace(got.IncompleteSuffixes) == "" {
		t.Fatal("incomplete_suffixes 被清空（保险丝字段被重置）")
	}
	if got.OpsPerSec != before.OpsPerSec {
		t.Fatalf("ops_per_sec=%.2f，期望保留 %.2f", got.OpsPerSec, before.OpsPerSec)
	}
	if len(got.Tasks) != 2 {
		t.Fatalf("tasks=%v，期望写入 2 项", got.Tasks)
	}
}

// TestSmoke_SaveEmptyTasksPersists 验证显式清空目录后能持久化（不再回填 ["/"]）。
func TestSmoke_SaveEmptyTasksPersists(t *testing.T) {
	defer withTempPaths(t)()
	defer selfCheckWG.Wait() // 等 handleSave 的「保存后自检」goroutine 收敛，避免污染真实日志
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	// 先写入两个目录。
	rec1 := httptest.NewRecorder()
	handleSave(rec1, httptest.NewRequest("POST", "/api/save", strings.NewReader(`{"tasks":["/电影"]}`)))
	if rec1.Code != 200 {
		t.Fatalf("first save code=%d body=%s", rec1.Code, rec1.Body.String())
	}
	if len(currentConfig().Tasks) != 1 {
		t.Fatalf("写入后 tasks=%v，期望 1 项", currentConfig().Tasks)
	}
	// 再显式清空。
	rec2 := httptest.NewRecorder()
	handleSave(rec2, httptest.NewRequest("POST", "/api/save", strings.NewReader(`{"tasks":[]}`)))
	if rec2.Code != 200 {
		t.Fatalf("second save code=%d body=%s", rec2.Code, rec2.Body.String())
	}
	if len(currentConfig().Tasks) != 0 {
		t.Fatalf("清空后内存 tasks=%v，期望为空", currentConfig().Tasks)
	}
	// 重新加载，确认落盘为空（未被 normalize 回填 ["/"]）。
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := currentConfig().Tasks; len(got) != 0 {
		t.Fatalf("重载后 tasks=%v，期望为空（已被回填！）", got)
	}
}

// TestSmoke_SavePartialThenEmptyTasks 组合：连续两次部分提交，空目录最终生效。
func TestSmoke_SavePartialThenEmptyTasks(t *testing.T) {
	defer withTempPaths(t)()
	defer selfCheckWG.Wait() // 等 handleSave 的「保存后自检」goroutine 收敛，避免污染真实日志
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	for _, body := range []string{`{"tasks":["/a"]}`, `{"tasks":[]}`, `{"tasks":[]}`} {
		rec := httptest.NewRecorder()
		handleSave(rec, httptest.NewRequest("POST", "/api/save", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("save %s code=%d body=%s", body, rec.Code, rec.Body.String())
		}
	}
	if got := currentConfig().Tasks; len(got) != 0 {
		t.Fatalf("最终 tasks=%v，期望为空", got)
	}
}

// TestSmoke_SweeperRunEmptyTasksFriendlyError 验证 sweeper.run 在空目录时
// 直接返回明确的中文错误（T7 后端保险）。
func TestSmoke_SweeperRunEmptyTasksFriendlyError(t *testing.T) {
	sw := newSweeper(nil, Config{Tasks: []string{}}, &TokenInfo{})
	_, err := sw.run(context.Background(), false)
	if err == nil {
		t.Fatal("空目录时 sweeper.run 应返回错误")
	}
	want := "未配置任何目录，请先在「连接与目录」中添加要清理的目录"
	if err.Error() != want {
		t.Fatalf("错误信息=%q，期望 %q", err.Error(), want)
	}
}

// TestSmoke_SaveMergePreservesMaxDepth 证明合并解码不会把 max_depth 抹成 0（= 不限递归深度），
// 它是「保存即重置」缺陷中最危险的字段（0 表示不限深，误置会放大 API 调用与风控风险）。
func TestSmoke_SaveMergePreservesMaxDepth(t *testing.T) {
	defer withTempPaths(t)()
	defer selfCheckWG.Wait() // 等 handleSave 的「保存后自检」goroutine 收敛，避免污染真实日志
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	// 先显式写入非零 max_depth。
	rec1 := httptest.NewRecorder()
	handleSave(rec1, httptest.NewRequest("POST", "/api/save", strings.NewReader(`{"max_depth":3}`)))
	if rec1.Code != 200 {
		t.Fatalf("save max_depth code=%d body=%s", rec1.Code, rec1.Body.String())
	}
	if currentConfig().MaxDepth != 3 {
		t.Fatalf("写入后 max_depth=%d，期望 3", currentConfig().MaxDepth)
	}
	// 再只提交 tasks，max_depth 必须保留 3 而非被清零。
	rec2 := httptest.NewRecorder()
	handleSave(rec2, httptest.NewRequest("POST", "/api/save", strings.NewReader(`{"tasks":["/电影"]}`)))
	if rec2.Code != 200 {
		t.Fatalf("partial save code=%d body=%s", rec2.Code, rec2.Body.String())
	}
	if got := currentConfig().MaxDepth; got != 3 {
		t.Fatalf("部分提交后 max_depth=%d，期望保留 3（被合并解码重置！）", got)
	}
	// 其余保险丝字段也应保留为非零默认值。
	if currentConfig().MaxFilesPerRun <= 0 || currentConfig().MaxTotalBytes <= 0 || currentConfig().Burst <= 0 {
		t.Fatalf("保险丝字段被重置：max_files=%d max_bytes=%d burst=%d",
			currentConfig().MaxFilesPerRun, currentConfig().MaxTotalBytes, currentConfig().Burst)
	}
}
