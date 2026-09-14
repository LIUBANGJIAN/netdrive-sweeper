package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const appName = "NetDrive Sweeper"

var (
	configPath  = getenv("CONFIG_PATH", "data/config.json")
	recordsPath = getenv("RECORDS_PATH", "data/records.jsonl")
	logPath     = getenv("LOG_PATH", "data/clean.log")
	listenAddr  = getenv("LISTEN", ":5000")
)

var (
	stateMu    sync.Mutex
	cfg        = defaultConfig()
	statusInfo = RuntimeStatus{LastMessage: "未连接"}
	scanBusy   bool

	// lastResult / lastScanAt 记录最近一次扫描/清理结果（含事件驱动后台触发），
	// 供前端打开页面时自动呈现，避免「事件已在后台跑、页面却空空如也」。
	lastResult *ScanResult
	lastScanAt time.Time
)

// 应用级根上下文：用于常驻的 PushMessage 事件驱动订阅，随进程生命周期存续。
var (
	rootCtx, rootCancel = context.WithCancel(context.Background())
)

// RuntimeStatus 是页面顶部状态。
type RuntimeStatus struct {
	Running     bool       `json:"running"`
	LastMessage string     `json:"last_message"`
	Token       *TokenInfo `json:"token,omitempty"`
	DeleteMode  string     `json:"deleteMode,omitempty"`
}

func main() {
	if err := mustLoadConfig(); err != nil {
		appendLog("配置加载警告: %v", err)
	}
	// 事件驱动实时清理（P0-27）：常驻订阅 CD2 PushMessage，是替代定时轮询的唯一合法实时感知方式。
	if currentConfig().EnablePush {
		go startPushConsumer(rootCtx)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/state", handleState)
	mux.HandleFunc("/api/save", handleSave)
	mux.HandleFunc("/api/test", handleTest)
	mux.HandleFunc("/api/list", handleList)
	mux.HandleFunc("/api/scan", handleScan)
	mux.HandleFunc("/api/clean", handleClean)
	mux.HandleFunc("/api/records", handleRecords)
	mux.HandleFunc("/api/logs", handleLogs)
	mux.HandleFunc("/api/clear_logs", handleClearLogs)
	mux.HandleFunc("/api/last_scan", handleLastScan)

	log.Printf("%s 启动，监听 %s，配置文件: %s", appName, listenAddr, configPath)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatal(err)
	}
}

func setStatus(message string, token *TokenInfo) {
	stateMu.Lock()
	defer stateMu.Unlock()
	statusInfo.LastMessage = message
	statusInfo.DeleteMode = map[bool]string{true: "permanent", false: "recycle"}[cfg.DeletePermanently]
	if token != nil {
		statusInfo.Token = token
	}
}

func appendLog(format string, args ...any) {
	if err := ensureDataDirs(); err != nil {
		log.Printf("日志目录创建失败: %v", err)
		return
	}
	line := time.Now().Format("2006-01-02 15:04:05") + " " + fmt.Sprintf(format, args...) + "\n"
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
	log.Print(strings.TrimSpace(line))
}

func formatError(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// ---------- handlers ----------

func handleIndex(w http.ResponseWriter, r *http.Request) {
	_ = pageTpl.Execute(w, map[string]any{"Title": appName})
}

func handleState(w http.ResponseWriter, r *http.Request) {
	stateMu.Lock()
	c := cfg
	s := statusInfo
	stateMu.Unlock()
	writeJSON(w, map[string]any{"config": c, "status": s})
}

func handleSave(w http.ResponseWriter, r *http.Request) {
	// 以当前配置为基底做「合并解码」：请求体里未携带的字段保留现值，
	// 避免前端只提交部分字段时把其余字段静默重置为零值（同类问题见 FIX-A）。
	stateMu.Lock()
	next := cfg
	stateMu.Unlock()
	if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
		writeError(w, formatError("配置解析失败: %s", err))
		return
	}
	// T7 裁决：空目录 = 不扫描任何目录。此处不再回填 ["/"]，
	// 允许用户显式清空任务目录；空列表由 sweeper.run 给出明确错误提示。
	next = normalizeConfig(next)
	stateMu.Lock()
	cfg = next
	err := saveConfigLocked()
	stateMu.Unlock()
	if err != nil {
		writeError(w, formatError("配置保存失败: %s", err))
		return
	}
	appendLog("配置已保存（限速 %.1f ops/s，删除方式 %s）", next.OpsPerSec, map[bool]string{true: "永久", false: "回收站"}[next.DeletePermanently])
	writeJSON(w, map[string]any{"ok": true, "config": next})
}

func handleTest(w http.ResponseWriter, r *http.Request) {
	client, err := newCD2Client(currentConfig())
	if err != nil {
		writeError(w, err)
		return
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := client.TCPCheck(ctx); err != nil {
		writeError(w, formatCD2Error(err))
		return
	}
	info, err := client.TokenInfo(ctx)
	if err != nil {
		writeError(w, formatCD2Error(err))
		return
	}
	msg := "连接成功，Token 根目录: " + info.RootDir
	setStatus(msg, info)
	appendLog("连接测试成功 %s", msg)
	writeJSON(w, map[string]any{"ok": true, "token": info, "message": msg})
}

func handleList(w http.ResponseWriter, r *http.Request) {
	path := normalizePath(r.URL.Query().Get("path"))
	client, token, err := readyClient(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	defer client.Close()
	files, err := client.List(r.Context(), path)
	if err != nil {
		writeError(w, formatCD2Error(err))
		return
	}
	dirs := make([]FileItem, 0)
	for _, f := range files {
		if f.IsDir && !strings.HasPrefix(f.Name, ".") {
			f.DisplayPath = displayPath(token, f.Path)
			dirs = append(dirs, f)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	writeJSON(w, map[string]any{"ok": true, "path": path, "displayPath": displayPath(token, path), "dirs": dirs, "token": token})
}

func handleScan(w http.ResponseWriter, r *http.Request) {
	// T7：空目录 = 不扫描。在连接 CD2 之前先给出准确诊断，
	// 避免「未配置任何目录」被误报成 CD2 连接错误。
	if len(currentConfig().Tasks) == 0 {
		writeError(w, errors.New("未配置任何目录，请先在「连接与目录」中添加要清理的目录"))
		return
	}
	res, err := runScan(r.Context(), false)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, res)
}

func handleClean(w http.ResponseWriter, r *http.Request) {
	// T7：空目录 = 不扫描。同 handleScan，先于 CD2 连接给出准确诊断。
	if len(currentConfig().Tasks) == 0 {
		writeError(w, errors.New("未配置任何目录，请先在「连接与目录」中添加要清理的目录"))
		return
	}
	res, err := runScan(r.Context(), true)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, res)
}

func handleRecords(w http.ResponseWriter, r *http.Request) {
	b, _ := os.ReadFile(recordsPath)
	lines := strings.Split(string(b), "\n")
	out := make([]json.RawMessage, 0, len(lines))
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		out = append(out, json.RawMessage(ln))
	}
	writeJSON(w, map[string]any{"ok": true, "records": out, "count": len(out)})
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	b, _ := os.ReadFile(logPath)
	writeJSON(w, map[string]string{"logs": string(b)})
}

func handleLastScan(w http.ResponseWriter, r *http.Request) {
	stateMu.Lock()
	res := lastResult
	at := lastScanAt
	stateMu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "result": res, "at": at})
}

func handleClearLogs(w http.ResponseWriter, r *http.Request) {
	ensureDataDirs()
	_ = os.WriteFile(logPath, nil, 0644)
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- helpers ----------

func readyClient(ctx context.Context) (*CD2Client, *TokenInfo, error) {
	client, err := newCD2Client(currentConfig())
	if err != nil {
		return nil, nil, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := client.TCPCheck(checkCtx); err != nil {
		client.Close()
		return nil, nil, formatCD2Error(err)
	}
	token, err := client.TokenInfo(checkCtx)
	if err != nil {
		client.Close()
		return nil, nil, formatCD2Error(err)
	}
	if !token.AllowList {
		client.Close()
		return nil, nil, formatError("Token 缺少 allow_list 权限，无法读取目录")
	}
	setStatus("已连接: "+token.RootDir, token)
	return client, token, nil
}

func runScan(ctx context.Context, deleteMode bool) (*ScanResult, error) {
	stateMu.Lock()
	if scanBusy {
		stateMu.Unlock()
		return nil, formatError("已有扫描任务运行中，请稍后再试")
	}
	scanBusy = true
	statusInfo.Running = true
	stateMu.Unlock()
	defer func() {
		stateMu.Lock()
		scanBusy = false
		statusInfo.Running = false
		stateMu.Unlock()
	}()

	client, token, err := readyClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	cfg := currentConfig()
	sw := newSweeper(client, cfg, token)
	res, err := sw.run(ctx, deleteMode)
	if err == nil {
		// 记录最近一次结果，供前端自动呈现（含事件驱动后台触发）。
		stateMu.Lock()
		lastResult = res
		lastScanAt = time.Now()
		stateMu.Unlock()
	}
	return res, err
}

// startPushConsumer 常驻运行事件驱动实时清理。连接失败会记日志并每 10s 重试，
// 绝不 panic / log.Fatal。权限不足时记为不可用并退出（不影响手动扫描）。
func startPushConsumer(ctx context.Context) {
	// 配置缺失（地址/Token 为空）是永久性错误，不是临时网络故障：
	// 不进入重连循环，否则会每 10s 刷一条「连接失败」噪声日志。
	// 打一条明确提示即返回；配置补全后需重启生效。
	c := currentConfig()
	if normalizeAddress(c.Address) == "" || strings.TrimSpace(c.Token) == "" {
		appendLog("事件驱动实时清理未启动：CD2 地址或 Token 未配置（配置后需重启生效）")
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		client, token, err := connectPushClient(ctx)
		if err != nil {
			appendLog("事件驱动实时清理连接失败：%v（10s 后重试）", err)
			if !waitOrDone(ctx, 10*time.Second) {
				return
			}
			continue
		}
		if !token.AllowPushMessage {
			client.Close()
			appendLog("未授予 allow_push_message 权限，事件驱动实时清理不可用（不影响手动扫描）")
			return
		}
		debounce := time.Duration(currentConfig().PushDebounceSeconds) * time.Second
		p := newPushConsumer(client, debounce, func() {
			appendLog("收到文件系统变更事件，防抖后触发扫描")
			// runScan 自带 scanBusy 互斥，进行中会返回「已有扫描任务」错误。
			res, serr := runScan(ctx, currentConfig().AllowDelete)
			if serr != nil {
				appendLog("事件触发扫描跳过: %v", serr)
				return
			}
			appendLog("事件触发扫描完成 checked=%d matched=%d deleted=%d", res.Checked, res.Matched, res.Deleted)
		})
		appendLog("事件驱动实时清理已启动（PushMessage，防抖 %ds）", currentConfig().PushDebounceSeconds)
		p.run(ctx) // 常驻订阅；内部自带重连退避，ctx 取消时返回
		client.Close()
		return
	}
}

// connectPushClient 建立并校验一次 PushMessage 订阅所需的连接（TCP 探测 + Token 校验）。
// 失败时已关闭 client，返回的 client 为 nil。
func connectPushClient(ctx context.Context) (*CD2Client, *TokenInfo, error) {
	client, err := newCD2Client(currentConfig())
	if err != nil {
		return nil, nil, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := client.TCPCheck(checkCtx); err != nil {
		client.Close()
		return nil, nil, formatCD2Error(err)
	}
	token, err := client.TokenInfo(checkCtx)
	if err != nil {
		client.Close()
		return nil, nil, formatCD2Error(err)
	}
	return client, token, nil
}

// waitOrDone 等待 d 时长；若 ctx 先被取消则返回 false。
func waitOrDone(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
}
