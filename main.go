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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// PushRuntime 描述事件驱动订阅（PushMessage）的实时状态，供页面直接展示。
// 有了它，用户不必再靠猜：配置一保存就会热启动/热重启订阅，页面立刻能看到
// 「运行中 / 未启用 / 权限不足 / 连接失败重试中」。这是修复「改配置后必须重启容器、
// 否则事件驱动静默失效且毫无解释」的关键——把隐性约束变成显式状态。
type PushRuntime struct {
	State     string `json:"state"`     // off|config_missing|connecting|running|denied|error
	Detail    string `json:"detail"`    // 人类可读说明
	Since     string `json:"since"`     // 进入该状态的时间
	Events    int    `json:"events"`    // 已收到的文件系统变更事件数
	LastEvent string `json:"lastEvent"` // 最近一次文件系统变更事件时间
}

var (
	pushMu   sync.Mutex
	pushStop context.CancelFunc // 当前订阅的取消函数；nil 表示没有在跑
	pushSig  string             // 当前订阅启动时对应的配置指纹
	pushWake = make(chan struct{}, 1)
	pushStat = PushRuntime{State: "off", Detail: "未启用"}
	// pushRev 每次保存配置 +1，使订阅指纹必然变化 → 触发热重启。
	// 这样「在 CD2 里给 Token 补上 allow_push_message 后回页面保存」即可生效，无需重启容器。
	pushRev atomic.Int64
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
	// 由 pushSupervisor 统一管理：配置一旦保存即热启动 / 热重启，无需重启容器。
	go pushSupervisor(rootCtx)
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
	mux.HandleFunc("/api/push", handlePush)

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
	// 页面是内联单文件（HTML+CSS+JS 一体），必须禁缓存，否则修好的前端逻辑会被旧副本掩盖。
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	_ = pageTpl.Execute(w, map[string]any{"Title": appName})
}

func handleState(w http.ResponseWriter, r *http.Request) {
	stateMu.Lock()
	c := cfg
	s := statusInfo
	at := lastScanAt
	stateMu.Unlock()
	writeJSON(w, map[string]any{"config": c, "status": s, "push": pushSnapshot(), "lastScanAt": at})
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
	// 唤醒事件驱动监督器：地址 / Token / 开关 / 防抖任一变化都会热重启订阅，无需重启容器。
	wakePushSupervisor()
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

// handlePush 返回事件驱动订阅的实时状态（纯内存读取，供前端轻量轮询）。
func handlePush(w http.ResponseWriter, r *http.Request) {
	stateMu.Lock()
	at := lastScanAt
	stateMu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "push": pushSnapshot(), "lastScanAt": at})
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

// ---------- 事件驱动实时清理：监督器 + 订阅 ----------

// pushSnapshot 返回一份推送运行时状态快照（供 /api/state 输出）。
func pushSnapshot() PushRuntime {
	pushMu.Lock()
	defer pushMu.Unlock()
	return pushStat
}

// setPushState 更新推送运行时状态（自持锁）。注意：不要在本函数内再取 pushMu 之外的锁后回调，
// 也不要在持有 pushMu 时调用（否则死锁），持有锁时用 setPushStateLocked。
func setPushState(state, detail string) {
	pushMu.Lock()
	setPushStateLocked(state, detail)
	pushMu.Unlock()
}

func setPushStateLocked(state, detail string) {
	pushStat.State = state
	pushStat.Detail = detail
	pushStat.Since = time.Now().Format("2006-01-02 15:04:05")
}

// bumpPushEvent 记录一次文件系统变更事件。
func bumpPushEvent() {
	pushMu.Lock()
	pushStat.Events++
	pushStat.LastEvent = time.Now().Format("2006-01-02 15:04:05")
	pushMu.Unlock()
}

// wakePushSupervisor 非阻塞唤醒监督器，使其按最新配置热重启订阅。
func wakePushSupervisor() {
	pushRev.Add(1)
	select {
	case pushWake <- struct{}{}:
	default:
	}
}

// pushConfigSignature 返回影响订阅的配置指纹。含 revision，因此每次「保存配置」都会
// 触发一次热重启——这正是想要的语义：保存即生效，不必重启容器。
func pushConfigSignature(c Config) string {
	return normalizeAddress(c.Address) + "\x00" + strings.TrimSpace(c.Token) + "\x00" +
		strconv.FormatBool(c.EnablePush) + "\x00" + strconv.Itoa(c.PushDebounceSeconds) + "\x00" +
		strconv.FormatInt(pushRev.Load(), 10)
}

// pushSupervisor 常驻，按当前配置按需启动 / 停止 / 重启 PushMessage 订阅。
// 触发时机：进程启动 + 每次保存配置（唤醒） + 20s 兜底巡检。
// 它只读内存配置、不做任何网络轮询，因此与「禁止定时扫全树」的风控约束不冲突。
func pushSupervisor(ctx context.Context) {
	for {
		c := currentConfig()
		want := c.EnablePush && normalizeAddress(c.Address) != "" && strings.TrimSpace(c.Token) != ""
		sig := pushConfigSignature(c)

		pushMu.Lock()
		running := pushStop != nil
		if running && (!want || pushSig != sig) {
			pushStop()
			pushStop = nil
			pushSig = ""
			running = false
			if !want {
				setPushStateLocked("config_missing", pushDisabledReason(c))
			} else {
				setPushStateLocked("off", "正在按新配置重启订阅…")
			}
		}
		if want && !running {
			cctx, cancel := context.WithCancel(ctx)
			pushStop = cancel
			pushSig = sig
			go runPushConsumer(cctx, c)
		} else if !want {
			// 仅在状态变化时写入，避免 20s 巡检反复刷新 Since。
			reason := pushDisabledReason(c)
			if pushStat.State != "config_missing" || pushStat.Detail != reason {
				setPushStateLocked("config_missing", reason)
			}
		}
		pushMu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-pushWake:
		case <-time.After(20 * time.Second):
		}
	}
}

// runPushConsumer 常驻运行事件驱动实时清理，直到 ctx 取消。
// 连接失败记日志并每 10s 重试；权限不足记日志并返回（由监督器在下次保存配置后重启）。
func runPushConsumer(ctx context.Context, c Config) {
	setPushState("connecting", "正在连接 CD2…")
	debounce := time.Duration(c.PushDebounceSeconds) * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		client, token, err := connectPushClient(ctx, c)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			setPushState("error", "连接失败："+formatCD2Error(err).Error()+"（10s 后重试）")
			appendLog("事件驱动实时清理连接失败：%v（10s 后重试）", err)
			if !waitOrDone(ctx, 10*time.Second) {
				return
			}
			continue
		}
		if !token.AllowPushMessage {
			client.Close()
			setPushState("denied", "Token 缺少 allow_push_message 权限，事件驱动不生效")
			appendLog("Token 缺少 allow_push_message 权限，事件驱动实时清理不可用（不影响手动扫描）。" +
				"在 CD2 为该 Token 勾选该权限后回本页「保存配置」即可生效，无需重启容器")
			return
		}
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
		setPushState("running", fmt.Sprintf("运行中（PushMessage 已订阅，防抖 %ds）", c.PushDebounceSeconds))
		appendLog("事件驱动实时清理已启动（PushMessage，防抖 %ds）", c.PushDebounceSeconds)
		p.run(ctx) // 常驻订阅；内部自带重连退避，ctx 取消时返回
		client.Close()
		setPushState("off", "订阅已结束")
		return
	}
}

// pushDisabledReason 给出「未启动订阅」的可读原因。
func pushDisabledReason(c Config) string {
	if !c.EnablePush {
		return "未启用（已关闭「事件驱动实时清理」）"
	}
	return "未启用：CD2 地址或 Token 未配置"
}

// connectPushClient 建立并校验一次 PushMessage 订阅所需的连接（TCP 探测 + Token 校验）。
// 失败时已关闭 client，返回的 client 为 nil。
func connectPushClient(ctx context.Context, c Config) (*CD2Client, *TokenInfo, error) {
	client, err := newCD2Client(c)
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
