package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const appName = "NetDrive Sweeper"

// appVersion 是产品语义化版本号，显示在页面左上角标题旁，便于用户区分部署的版本。
// 构建时可用 -ldflags "-X main.appVersion=x.y.z" 覆盖（Dockerfile / CI 均从仓库根的 VERSION
// 文件注入）；未覆盖时回落到下面的默认值。修改版本号时请同时更新 VERSION 文件。
var appVersion = "1.1.0"

// versionLabel 是用于展示的完整版本串：v<版本号>，并在构建信息可得时附上 VCS 修订短 SHA
// （形如 v1.0.0 (a1b2c3d)，工作区有未提交改动时带 -dirty）。进程启动时计算一次，
// 避免每次页面渲染都调用 debug.ReadBuildInfo。
var versionLabel = func() string {
	label := "v" + appVersion
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return label
	}
	var rev, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				modified = "-dirty"
			}
		}
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if rev != "" {
		label += " (" + rev + modified + ")"
	}
	return label
}()

// intervalText 把「分钟」配置渲染为可读文本；0 或负数表示关闭。
func intervalText(minutes int) string {
	if minutes <= 0 {
		return "关闭"
	}
	return fmt.Sprintf("每 %d 分钟", minutes)
}

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
	State           string        `json:"state"`                     // off|config_missing|connecting|running|denied|error
	Detail          string        `json:"detail"`                    // 人类可读说明
	Since           string        `json:"since"`                     // 进入该状态的时间
	Events          int           `json:"events"`                    // 已收到的（范围内）文件系统变更事件数
	IgnoredEvents   int           `json:"ignoredEvents"`             // 被忽略的「清理范围外/无路径」变更事件数（F1/F3）
	LastEvent       string        `json:"lastEvent"`                 // 最近一次文件系统变更事件时间
	LastEventPath   string        `json:"lastEventPath,omitempty"`   // 最近一次文件系统变更事件的路径（尽力而为提取）
	LastMessageAt   string        `json:"lastMessageAt,omitempty"`   // 最近一次收到任意类型推送消息的时间（含 CD2 自身日志广播，仅证明 gRPC 流活着）
	LastFileEventAt string        `json:"lastFileEventAt,omitempty"` // 最近一次收到 FILE_SYSTEM_CHANGE(=4) 的时间（文件事件通道存活的证据，与 LastMessageAt 严格区分）
	TypeCounts      map[int32]int `json:"typeCounts,omitempty"`      // 各 messageType 的累计到达次数

	// —— 存活可观测性（D5）：让前端能区分「流活着但恰好没事件」与「流已经死了」 ——
	Gen          int64  `json:"gen"`                    // 当前订阅世代（每次起/重启 +1；用于识别过期写入）
	SubscribedAt string `json:"subscribedAt,omitempty"` // 本次订阅最近一次成功进入 running 的时间
	Reconnects   int    `json:"reconnects"`             // 订阅中断后的重连计数
	LastError    string `json:"lastError,omitempty"`    // 最近一次订阅失败原因（已脱敏，绝不写 Token 明文）
	LastErrorAt  string `json:"lastErrorAt,omitempty"`  // 最近一次订阅失败时间
}

var (
	pushMu   sync.Mutex
	pushStop context.CancelFunc // 当前订阅的取消函数；nil 表示没有在跑
	pushSig  string             // 当前订阅启动时对应的配置指纹
	// pushDone 由监督器在启动消费者时创建、并在同一临界区内赋值；消费者退出时关闭它。
	// 它是「自称在跑、其实已死」的唯一可信判据（D2 看门狗）：pushStop 非 nil 但 pushDone 已关闭
	// ⇒ 消费者 goroutine 已意外退出，监督器应立即重建订阅（最迟 20s 自愈）。
	pushDone chan struct{}
	pushWake = make(chan struct{}, 1)
	pushStat = PushRuntime{State: "off", Detail: "未启用"}
	// pushRev 每次保存配置 +1，使订阅指纹必然变化 → 触发热重启。
	// 这样「在 CD2 里给 Token 补上 allow_push_message 后回页面保存」即可生效，无需重启容器。
	pushRev atomic.Int64
	// pushGen 是订阅世代计数器：每次起/重启消费者 +1。用于世代守卫（D1/D3）——
	// 旧世代 goroutine 的收尾（如 setPushState("off")）不得覆盖新世代写入的 running。
	pushGen atomic.Int64
)

// pushLaunch 是「启动一个消费者 goroutine」的间接层（默认即 go runPushConsumer）。
// 抽成变量是为了让单测能注入假消费者，从而确定性地验证看门狗（D2）的“死亡→重建”行为，
// 而无需真的去连 CD2。生产路径行为完全一致。
var pushLaunch = func(ctx context.Context, c Config, gen int64, done chan struct{}) {
	go runPushConsumer(ctx, c, gen, done)
}

// 应用级根上下文：用于常驻的 PushMessage 事件驱动订阅，随进程生命周期存续。
var (
	rootCtx, rootCancel = context.WithCancel(context.Background())
)

// selfCheckWG 供测试等待 handleSave 触发的「保存后自检」异步 goroutine 结束。
// 若不等待，该 goroutine 可能在测试清理（恢复 logPath 到 data/clean.log）之后才写日志，
// 把「保存后自检：CD2 暂不可达…」这类测试行污染进真实日志，干扰对线上故障的判断。
// 生产环境无人调用 Wait，等同空操作，无行为影响。
var selfCheckWG sync.WaitGroup

// RuntimeStatus 是页面顶部状态。
type RuntimeStatus struct {
	Running     bool       `json:"running"`
	LastMessage string     `json:"last_message"`
	Token       *TokenInfo `json:"token,omitempty"`
	DeleteMode  string     `json:"deleteMode,omitempty"`
	CloudAPIs   []CloudAPI `json:"cloudApis,omitempty"` // 各云盘连接与云端事件监听器状态
}

func main() {
	if err := mustLoadConfig(); err != nil {
		appendLog("配置加载警告: %v", err)
	}
	// 启动即写入一条可读的系统运行摘要，让页面「运行日志」能反映系统运行情况
	// （此前只走 log.Printf/stdout，页面读的是 data/clean.log，用户看不到）。
	cfg := currentConfig()
	delMode := "回收站"
	if cfg.DeletePermanently {
		delMode = "永久删除"
	}
	appendLog("系统启动 | 版本=%s 地址=%s 事件驱动=%v 防抖=%ds 冷却=%dh 删除方式=%s 允许删除=%v 清理目录=%d 个",
		versionLabel, cfg.Address, cfg.EnablePush, cfg.PushDebounceSeconds, cfg.FileCooldownHours, delMode, cfg.AllowDelete, len(cfg.Tasks))
	appendLog("兜底策略 | 离线任务监控=%s 事件静默兜底=%s（0=关闭；均可随时在页面「清理规则」调整）",
		intervalText(cfg.OfflineMonitorMinutes), intervalText(cfg.EventFallbackScanMinutes))
	// 事件驱动实时清理（P0-27）：常驻订阅 CD2 PushMessage，是替代定时轮询的唯一合法实时感知方式。
	// 由 pushSupervisor 统一管理：配置一旦保存即热启动 / 热重启，无需重启容器。
	go pushSupervisor(rootCtx)
	// 启动即常驻自检 CD2 连接与 Token 权限：这样容器启动 / 更新镜像后，页面无需手动点
	// 「测试连接」就能显示连接状态与权限徽章；CD2 晚启动或中途重启也能自动恢复。
	go statusMonitor(rootCtx)
	// 离线任务监控：周期性查询清理目录的离线下载状态，检测「下载中→完成」翻转即触发扫描。
	// 这是事件驱动（依赖 CD2 推送）在云端监听器未运行时的快速兜底（最快约 1 分钟），比
	// 事件静默兜底扫描（15 分钟）更快；也是对「15 分钟静默兜底」之外用户拍板需求的落地。
	go offlineMonitor(rootCtx)
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
	_ = pageTpl.Execute(w, map[string]any{"Title": appName, "Version": versionLabel})
}

func handleState(w http.ResponseWriter, r *http.Request) {
	stateMu.Lock()
	c := cfg
	s := statusInfo
	at := lastScanAt
	stateMu.Unlock()
	writeJSON(w, map[string]any{"config": c, "status": s, "push": pushSnapshot(), "offlineMonitor": offlineMonSnapshot(), "lastScanAt": at, "inContainer": inContainer()})
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
	// 保存后立刻自检一次连接与权限：用户改完地址/Token 无需再手点「测试连接」。
	// selfCheckWG 让测试能等待该异步任务收敛，避免其在测试恢复 logPath 之后才写日志、
	// 从而污染真实 data/clean.log（生产环境无人 Wait，等同空操作）。
	selfCheckWG.Add(1)
	go func(c Config) {
		defer selfCheckWG.Done()
		if _, err := probeCD2Status(rootCtx, c); err != nil {
			appendLog("保存后自检：CD2 暂不可达（%v）", err)
		}
	}(next)
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
	appendLog("手动清理开始（仅扫描，未开启删除总开关）")
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
	delMode := "回收站"
	if currentConfig().DeletePermanently {
		delMode = "永久删除"
	}
	appendLog("手动清理开始（删除方式：%s）", delMode)
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
// 同时附带运行状态（含 Token 权限），使页面徽章在启动自检/保存自检后自动亮起，
// 不再需要用户手动点一次「测试连接」。
func handlePush(w http.ResponseWriter, r *http.Request) {
	stateMu.Lock()
	at := lastScanAt
	s := statusInfo
	stateMu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "push": pushSnapshot(), "offlineMonitor": offlineMonSnapshot(), "lastScanAt": at, "status": s, "inContainer": inContainer()})
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
// TypeCounts 做深拷贝：避免调用方在锁外 JSON 序列化时与写入方并发访问同一张 map。
func pushSnapshot() PushRuntime {
	pushMu.Lock()
	defer pushMu.Unlock()
	s := pushStat
	if pushStat.TypeCounts != nil {
		m := make(map[int32]int, len(pushStat.TypeCounts))
		for k, v := range pushStat.TypeCounts {
			m[k] = v
		}
		s.TypeCounts = m
	}
	return s
}

// pushEvidenceWindow 是「文件事件通道仍被证据支撑」的时间窗：最近一次收到
// FILE_SYSTEM_CHANGE(=4)（无论是否在清理范围内）在此窗口内，即说明 CD2 仍在投递文件变更事件。
const pushEvidenceWindow = 10 * time.Minute

// pushHasLiveEvidence 判断是否已有「CD2 正在投递文件变更事件」的确凿证据：订阅 running 且
// 最近一次收到 FILE_SYSTEM_CHANGE(=4) 在 pushEvidenceWindow 之内。
// 绝不能用 LastMessageAt（任意类型）作证据：LOG_MESSAGE=7 是 CD2 自身的日志广播，与各云盘
// 文件事件通道是否存活无关——用它会把「心跳还在、文件事件已断流」误判为健康（2026-09-17 线上实例）。
func pushHasLiveEvidence() bool {
	s := pushSnapshot()
	if s.State != "running" || s.LastFileEventAt == "" {
		return false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s.LastFileEventAt, time.Local)
	if err != nil {
		return false
	}
	return time.Since(t) <= pushEvidenceWindow
}

// lastFileEventT 读取最近一次 FILE_SYSTEM_CHANGE 的时刻（零值表示本进程尚未收到过）。
// 供事件静默兜底判定使用，与 pushHasLiveEvidence 共用同一证据来源。
func lastFileEventT() time.Time {
	s := pushSnapshot()
	if s.LastFileEventAt == "" {
		return time.Time{}
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s.LastFileEventAt, time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}

// fallbackScanDue 判定「事件静默兜底扫描」此刻是否应当执行（纯函数，便于表驱动单测）。
// 规则：
//   - interval<=0：关闭兜底；
//   - 静默起点 = 最近一次文件事件时刻；本进程尚未收到过则从 start（订阅建立时刻）起算；
//   - 距上次兜底扫描不足 interval 时跳过——持续静默时以 interval 为最小间隔重复兜底，
//     而不是每分钟都扫。
func fallbackScanDue(now, lastFileEvent, lastFallback, start time.Time, interval time.Duration) bool {
	if interval <= 0 {
		return false
	}
	if lastFileEvent.IsZero() {
		lastFileEvent = start
	}
	if now.Sub(lastFileEvent) < interval {
		return false
	}
	if !lastFallback.IsZero() && now.Sub(lastFallback) < interval {
		return false
	}
	return true
}

// runEventFallbackScanner 是「事件静默兜底扫描」循环：事件驱动订阅运行期间，若连续 interval
// 未收到任何 FILE_SYSTEM_CHANGE（典型原因：CD2 未上报云端事件通道，文件事件断流——这是
// CD2 服务端状态，本程序无从修复），自动执行一次扫描。每分钟检查一次；触发走与事件驱动相同的
// trigger（自带扫描互斥与 panic 兜底）。
// 背景：旧版（F1 范围过滤之前）任何 FSC 事件都会触发扫描，CD2 自身操作的涓流事件意外充当了
// 高频兜底；F1 精确化后，文件事件断流的实例会完全静默——本循环把兜底显式化且可控。
// 语义提示：文件事件长期断流时，本循环等价于「以 interval 为周期的一次扫描」（有界轮询，非无脑扫全树）。
// ctx 取消（配置热重启/停机）时退出，由调用方随新订阅世代重新拉起。
func runEventFallbackScanner(ctx context.Context, interval time.Duration, trigger func(), logf func(format string, args ...any)) {
	if interval <= 0 || trigger == nil {
		return
	}
	start := time.Now()
	var lastFallback time.Time
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now()
		if !fallbackScanDue(now, lastFileEventT(), lastFallback, start, interval) {
			continue
		}
		lastFallback = now
		if logf != nil {
			logf("事件静默已超过 %s（期间未收到任何文件变更事件），执行兜底扫描", interval)
		}
		trigger()
	}
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

// setPushStateIfGen 是「世代守卫」的状态写入：仅当 gen 仍是当前世代时才写，否则丢弃并返回 false。
// 用于消费者 goroutine 的所有状态写入（D1/D3）：旧世代在订阅被热重启后仍可能异步收尾，
// 若不加守卫，它写的 "off/订阅已结束" 会覆盖新世代刚写下的 "running"，导致 UI 无故翻成已停止。
// 进入 running 时顺带记录 SubscribedAt（本次订阅最近一次建立时间），供前端展示「已建立 Xm」。
func setPushStateIfGen(state, detail string, gen int64) bool {
	if gen != pushGen.Load() {
		return false // 快速路径：早已过期
	}
	pushMu.Lock()
	defer pushMu.Unlock()
	if gen != pushGen.Load() { // 取锁后世代可能已前进，二次确认
		return false
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	pushStat.State = state
	pushStat.Detail = detail
	pushStat.Since = now
	pushStat.Gen = gen
	if state == "running" {
		pushStat.SubscribedAt = now
	}
	return true
}

// markPushReconnect 记录订阅重连计数（世代守卫）：仅当前世代写入。
func markPushReconnect(gen int64, reconnects int) {
	if gen != pushGen.Load() {
		return
	}
	pushMu.Lock()
	defer pushMu.Unlock()
	if gen != pushGen.Load() {
		return
	}
	pushStat.Gen = gen
	pushStat.Reconnects = reconnects
}

// markPushLastError 记录最近一次订阅失败原因（世代守卫）。调用方须传入已脱敏的文本
// （如 formatCD2Error 的结果），绝不写 Token 明文。
func markPushLastError(gen int64, msg string) {
	if msg == "" {
		return
	}
	if gen != pushGen.Load() {
		return
	}
	pushMu.Lock()
	defer pushMu.Unlock()
	if gen != pushGen.Load() {
		return
	}
	pushStat.LastError = msg
	pushStat.LastErrorAt = time.Now().Format("2006-01-02 15:04:05")
}

// chanClosed 无阻塞判断一个 channel 是否已关闭（或将被 GC 的空通道视为未关闭）。
func chanClosed(ch chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// pushWatchdogLocked 是 D2 看门狗的核心判据：当「自称在跑」（pushStop != nil）但消费者
// goroutine 已退出（pushDone 已关闭）时，清空在跑标记并返回 (已死世代, true)，表示需要重建。
// 调用方必须持有 pushMu。抽成独立函数便于单测确定性地验证「死亡→重建」的判定。
func pushWatchdogLocked() (deadGen int64, dead bool) {
	if pushStop != nil && pushDone != nil && chanClosed(pushDone) {
		deadGen = pushStat.Gen
		pushStop()
		pushStop = nil
		pushSig = ""
		pushDone = nil
		return deadGen, true
	}
	return 0, false
}

// bumpPushEvent 记录一次文件系统变更事件。
func bumpPushEvent() {
	pushMu.Lock()
	pushStat.Events++
	pushStat.LastEvent = time.Now().Format("2006-01-02 15:04:05")
	pushMu.Unlock()
}

// markPushMessage 记录「收到任意类型推送消息」的时间与类型计数。
// 注意：任意消息（含 CD2 自身日志广播 LOG_MESSAGE=7）只能证明 gRPC 流活着，
// 不能证明文件变更事件仍在投递——后者见 markFileEvent / LastFileEventAt。
func markPushMessage(messageType int32) {
	pushMu.Lock()
	if pushStat.TypeCounts == nil {
		pushStat.TypeCounts = map[int32]int{}
	}
	pushStat.TypeCounts[messageType]++
	pushStat.LastMessageAt = time.Now().Format("2006-01-02 15:04:05")
	pushMu.Unlock()
}

// markFileEvent 记录「收到 FILE_SYSTEM_CHANGE(=4)」的时间（无论事件是否落在清理范围内）。
// 这是文件事件通道存活的唯一可信证据：LOG_MESSAGE=7 等日志广播与云盘文件事件通道是否
// 存活无关（2026-09-17 线上实例：四个云盘监听器全部未运行、文件事件断流，但 type=7 心跳
// 不断，曾据此误判「订阅存活，无需处理」）。
func markFileEvent() {
	pushMu.Lock()
	pushStat.LastFileEventAt = time.Now().Format("2006-01-02 15:04:05")
	pushMu.Unlock()
}

// markPushEventPath 记录最近一次文件系统变更事件的路径（尽力而为提取，空串则忽略）。
func markPushEventPath(path string) {
	if path == "" {
		return
	}
	pushMu.Lock()
	pushStat.LastEventPath = path
	pushMu.Unlock()
}

// bumpIgnoredEvent 记录一次「被忽略的清理范围外/无路径」变更事件（F1/F3），随 /api/state 暴露。
func bumpIgnoredEvent() {
	pushMu.Lock()
	pushStat.IgnoredEvents++
	pushMu.Unlock()
}

// currentTokenRoot 只读内存中的 Token 根目录（形如 /BON_115网盘），用于事件路径的范围判定。
// 绝不发起任何对 CD2 的请求。Token 未知时返回 ""。
func currentTokenRoot() string {
	stateMu.Lock()
	defer stateMu.Unlock()
	if statusInfo.Token == nil {
		return ""
	}
	return statusInfo.Token.RootDir
}

// setCloudAPIs 写入各云盘连接与云端事件监听器状态，供 /api/state 与前端 banner 展示。
func setCloudAPIs(apis []CloudAPI) {
	stateMu.Lock()
	statusInfo.CloudAPIs = apis
	stateMu.Unlock()
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
	lastReason := "" // 上次已记录的「未启动原因」：仅在变化时记日志，避免 20s 刷屏
	for {
		c := currentConfig()
		want := c.EnablePush && normalizeAddress(c.Address) != "" && strings.TrimSpace(c.Token) != ""
		sig := pushConfigSignature(c)

		deadLog := "" // 看门狗判定“假运行”时的诊断行（锁外记，避免持锁写日志）
		pushMu.Lock()
		running := pushStop != nil
		// D2 看门狗：自称在跑，但消费者 goroutine 已退出（done 已关闭）→ 判定「假运行」，立即重建。
		// 这是「更新 docker 后只运行一次」的机制性修复：只要 runPushConsumer 因任何原因返回
		//（ctx 取消 / 连接循环退出 / panic 恢复），done 就会关闭，监督器最迟在下一轮巡检（≤20s）重建。
		if deadGen, dead := pushWatchdogLocked(); dead {
			running = false
			deadLog = fmt.Sprintf("事件驱动订阅意外退出（世代 %d），正在自动重建…", deadGen)
		}
		if running && (!want || pushSig != sig) {
			pushStop()
			pushStop = nil
			pushSig = ""
			pushDone = nil
			running = false
			if !want {
				setPushStateLocked("config_missing", pushDisabledReason(c))
			} else {
				setPushStateLocked("off", "正在按新配置重启订阅…")
			}
		}
		if want && !running {
			g := pushGen.Add(1)
			cctx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			pushStop = cancel
			pushSig = sig
			pushDone = done
			pushLaunch(cctx, c, g, done)
		} else if !want {
			// 仅在状态变化时写入，避免 20s 巡检反复刷新 Since。
			reason := pushDisabledReason(c)
			if pushStat.State != "config_missing" || pushStat.Detail != reason {
				setPushStateLocked("config_missing", reason)
			}
		}
		pushMu.Unlock()

		if deadLog != "" {
			appendLog("%s", deadLog)
		}

		// 锁外记日志：仅在「未启动原因」发生变化（含进程启动后的首次巡检）时记一条完整诊断行。
		// 这样每次容器启动的日志里都有一行能回答：事件驱动是否启用 / 地址是什么 / Token 有没有 / 清理目录几个。
		line, newLast := pushSupervisorLogReason(want, c, lastReason)
		if line != "" {
			appendLog("%s", line)
		}
		lastReason = newLast

		select {
		case <-ctx.Done():
			return
		case <-pushWake:
		case <-time.After(20 * time.Second):
		}
	}
}

// pushSupervisorLogReason 决定本次巡检是否要写一条「事件驱动未启动」诊断行，并返回更新后的 lastReason。
// 抽成纯函数以便单测「状态变化时才记、不刷屏」。want=true（已启动）时返回空并复位 lastReason，
// 便于下次停止时重新记录。
//
// 判据不再只看 reason 文本，而是看 pushDisabledReasonKey 生成的「状态签名」：这样即便
// 原因文本偶有重合，只要关键字段（归一化地址 / Token 有-无 / 是否启用 / 清理目录数）发生变化，
// 也会重新记一条——例如「地址从空→有」「Token 从无→有」都能各自留下痕迹，而不会被静默吞掉。
func pushSupervisorLogReason(want bool, c Config, lastReason string) (line, newLast string) {
	if want {
		return "", ""
	}
	key := pushDisabledReasonKey(c)
	if key == lastReason {
		return "", lastReason
	}
	return pushDisableLogLine(c), key
}

// pushDisableLogLine 生成「事件驱动未启动」的完整诊断行。Token 仅打「有/无」，绝不回显明文。
func pushDisableLogLine(c Config) string {
	return fmt.Sprintf("事件驱动未启动：%s（地址=%q Token=%s 启用=%v 清理目录=%d 个）",
		pushDisabledReason(c), normalizeAddress(c.Address), tokenPresence(c.Token), c.EnablePush, len(cleanTasks(c.Tasks)))
}

// tokenPresence 返回 Token 的「有/无」描述，避免把 Token 明文写进日志。
func tokenPresence(tok string) string {
	if strings.TrimSpace(tok) == "" {
		return "无"
	}
	return "有"
}

// ---------- 容器环境与地址防呆（桥接模式 127.0.0.1 陷阱） ----------

// inContainer 报告当前进程是否运行在容器内（Docker / Podman 等）。
// 依据：容器运行时会在根目录放置标记文件 /.dockerenv（Docker）或 /run/.containerenv（Podman）。
// 这是「Docker 桥接模式下，容器内的 127.0.0.1 指向容器自身、永远连不到宿主机 CD2」这一
// 高频部署陷阱的防呆基础：前端据此在地址栏填回环地址时给出黄色内联提示，避免误导用户。
func inContainer() bool { return inContainerWith(os.Stat) }

// inContainerWith 是可注入 stat 的内层实现，便于单测确定性验证（无需真的在 / 下创建标记文件）。
func inContainerWith(stat func(string) (os.FileInfo, error)) bool {
	for _, p := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := stat(p); err == nil {
			return true
		}
	}
	return false
}

// isLoopbackAddr 判定 gRPC 地址的 host 部分是否为回环/通配地址
// （127.0.0.1 / localhost / ::1 / 0.0.0.0），并正确剥离端口、兼容无端口的裸 IPv6 字面量。
//
// 注意：前端内联 JS 里有一份语义完全一致的 isLoopbackAddr（用于「边输入边判定」——输入框的值
// 只有浏览器知道，后端无从获取）。此 Go 版本作为可表驱动验证的参考实现保留，两者规则必须同步，
// 见 web_addr_hint_test.go。
func isLoopbackAddr(addr string) bool {
	s := strings.TrimSpace(addr)
	if s == "" {
		return false
	}
	host := s
	if i := strings.Index(host, "://"); i >= 0 { // 容忍误填的 scheme 前缀
		host = host[i+3:]
	}
	if i := strings.IndexAny(host, "/?#"); i >= 0 { // 去掉路径/查询
		host = host[:i]
	}
	switch {
	case strings.HasPrefix(host, "["): // [::1]:19798 这类 IPv6 字面量
		if j := strings.IndexByte(host, ']'); j >= 0 {
			host = host[1:j]
		} else {
			host = host[1:]
		}
	case strings.Count(host, ":") > 1: // 多个冒号且无方括号：裸 IPv6，无端口，原样使用
	default:
		if i := strings.LastIndexByte(host, ':'); i >= 0 {
			port := host[i+1:]
			allDigit := port != ""
			for _, r := range port {
				if r < '0' || r > '9' {
					allDigit = false
					break
				}
			}
			if allDigit {
				host = host[:i]
			}
		}
	}
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "127.0.0.1", "localhost", "::1", "0.0.0.0":
		return true
	default:
		return false
	}
}

// pushConnect 是「建立一次订阅所需连接」的间接层（默认 connectPushClient）。
// 抽成变量以便单测确定性地触发 runPushConsumer 的 panic 兜底（D4），无需真实 CD2。
var pushConnect = connectPushClient

// runPushConsumer 常驻运行事件驱动实时清理，直到 ctx 取消。
// 连接失败记日志并每 10s 重试；权限不足记日志并 60s 退避重试（用户勾选权限后自愈）。
//
// gen 是订阅世代（D1/D3）：本函数所有 pushStat 写入都走 setPushStateIfGen，
// 过期世代（已被热重启替换）的收尾写入会被丢弃，绝不覆盖新世代状态。
// done 在本函数返回时关闭（任何退出路径，含 panic 恢复）——这是 D2 看门狗的「死亡」信号。
func runPushConsumer(ctx context.Context, c Config, gen int64, done chan struct{}) {
	defer close(done)
	exitReason := "未知"
	// 最外层 defer：panic 兜底（D4）+ 退出对账（D1）。
	// 注册顺序很关键：本 defer 在 close(done) 之后注册 → 后注册先执行，
	// 即先做 panic 恢复/写「已退出」状态，再关闭 done，保证监督器看到 done 关闭时状态已对账。
	defer func() {
		if r := recover(); r != nil {
			// 一次 panic 不再杀掉整个进程（此前会导致容器重启后「跑一次就死」）；记栈并交给监督器自愈。
			appendLog("事件驱动订阅发生 panic：%v\n%s", r, debug.Stack())
			exitReason = fmt.Sprintf("panic：%v", r)
		}
		// 仅对「非 ctx 取消」的退出做状态对账（panic / 订阅循环异常收尾）。
		// ctx 取消属于「被要求退出」（进程收尾 / 热重启），状态由监督器负责写入——若此处也写，
		// 会与热重启时的「正在按新配置重启订阅…」打架，导致状态无意义地翻成 off。
		if ctx.Err() == nil {
			setPushStateIfGen("off", "订阅已退出："+exitReason, gen)
		}
	}()

	setPushStateIfGen("connecting", "正在连接 CD2…", gen)
	debounce := time.Duration(c.PushDebounceSeconds) * time.Second
	for {
		if ctx.Err() != nil {
			exitReason = "ctx 取消"
			return
		}
		client, token, err := pushConnect(ctx, c)
		if err != nil {
			if ctx.Err() != nil {
				exitReason = "ctx 取消"
				return
			}
			msg := formatCD2Error(err).Error()
			setPushStateIfGen("error", "连接失败："+msg+"（10s 后重试）", gen)
			markPushLastError(gen, msg)
			appendLog("事件驱动实时清理连接失败：%v（10s 后重试）", err)
			if !waitOrDone(ctx, 10*time.Second) {
				exitReason = "ctx 取消"
				return
			}
			continue
		}
		if !token.AllowPushMessage {
			// 即便 Token 缺少消息推送权限，也要写入连接状态与权限徽章——否则用户
			// 「配好地址 + Token 后徽章仍不亮、必须手点测试连接」的问题依旧存在
			//（缺少 push 权限正是用户当前观察到的现象）。
			markPushDeniedConnected(token)
			client.Close()
			setPushStateIfGen("denied", "Token 缺少 allow_push_message 权限，事件驱动不生效", gen)
			markPushLastError(gen, "Token 缺少 allow_push_message 权限")
			appendLog("Token 缺少 allow_push_message 权限，事件驱动实时清理不可用（不影响手动清理）。" +
				"在 CD2 为该 Token 勾选该权限后会自动生效，无需保存配置或重启容器")
			// 不永久 return：60s 退避后重试，用户勾选 allow_push_message 后无需任何操作即可自愈。
			if !waitOrDone(ctx, 60*time.Second) {
				exitReason = "ctx 取消"
				return
			}
			continue
		}
		// 订阅成功即顺手刷新运行状态（含权限徽章），用的是本次已经取到的 Token，不额外发请求。
		setStatus("已连接: "+token.RootDir, token)
		// 先声明后赋值：让 trigger 闭包能引用 p 本身，从而在扫描互斥时 rearm 重试。
		var p *pushConsumer
		triggerFn := func() {
			// 扫描期间的 panic 不得把订阅 goroutine 一起带走：单独兜底（D4）。
			defer func() {
				if r := recover(); r != nil {
					appendLog("事件驱动触发扫描发生 panic：%v\n%s", r, debug.Stack())
				}
			}()
			appendLog("事件驱动：防抖后触发扫描")
			// runScan 自带 scanBusy 互斥，进行中会返回「已有扫描任务」错误。
			res, serr := runScan(ctx, currentConfig().AllowDelete)
			if serr != nil {
				// 扫描互斥不能丢弃本轮事件：若这是某次突发的最后一个事件，对应文件将
				// 永远不会被清理。此时延后一轮，在 debounce 后自动重试（rearm）。
				if strings.Contains(serr.Error(), "已有扫描任务") {
					appendLog("事件触发扫描延后（当前有扫描进行中），稍后将自动重试")
					p.rearm()
					return
				}
				appendLog("事件触发扫描跳过: %v", serr)
				return
			}
			appendLog("事件触发扫描完成 checked=%d matched=%d deleted=%d", res.Checked, res.Matched, res.Deleted)
		}
		p = newPushConsumer(client, debounce, triggerFn)
		p.gen = gen
		// F2：两次事件驱动扫描之间的最小间隔。0 = 关闭冷却（旧行为）。
		p.minInterval = time.Duration(c.EventScanMinIntervalMinutes) * time.Minute
		// 事件静默兜底：文件事件断流（如 CD2 云端监听器未运行）时按配置间隔自动扫描。
		// 这恢复了旧版「任何事件都触发扫描」的意外兜底能力，且不引入扫描风暴。
		go runEventFallbackScanner(ctx, time.Duration(c.EventFallbackScanMinutes)*time.Minute, triggerFn, appendLog)
		setPushStateIfGen("running", fmt.Sprintf("运行中（PushMessage 已订阅，防抖 %ds）", c.PushDebounceSeconds), gen)
		appendLog("事件驱动实时清理已启动（PushMessage，防抖 %ds，地址=%s）", c.PushDebounceSeconds, normalizeAddress(c.Address))
		p.run(ctx) // 常驻订阅；内部自带重连退避，ctx 取消时返回
		client.Close()
		exitReason = "订阅结束"
		return
	}
}

// pushDisabledReason 给出「未启动订阅」的可读原因。
// 四档文本互不相同，使日志能唯一区分失败模式：未启用 / 地址缺失 / Token 缺失 / 两者皆缺失。
// 注意：此前「地址缺失」与「Token 缺失」共用同一句「地址或 Token 未配置」，会导致
// 「地址为空 ↔ Token 为空」之间切换时 reason 文本不变、日志不重记，字段变化被吞掉。
func pushDisabledReason(c Config) string {
	if !c.EnablePush {
		return "未启用（已关闭「事件驱动实时清理」）"
	}
	addrEmpty := normalizeAddress(c.Address) == ""
	tokenEmpty := strings.TrimSpace(c.Token) == ""
	switch {
	case addrEmpty && tokenEmpty:
		return "未启用：CD2 地址与 Token 均未配置"
	case addrEmpty:
		return "未启用：CD2 地址未配置"
	case tokenEmpty:
		return "未启用：CD2 Token 未配置"
	default:
		// 地址与 Token 均已配置时 want=true，不会走到这里；保留可读兜底。
		return "未启用：CD2 地址与 Token 均未配置"
	}
}

// pushDisabledReasonKey 生成「未启动状态」的稳定签名，供 pushSupervisor 判定「是否需要重新记日志」。
// 相比只看 reason 文本，签名把关键字段也纳入比较，从而捕捉「同类原因下的字段级变化」：
// 归一化地址（空↔有）、Token 有/无、是否启用、清理目录数。
// 不包含 Token 明文——只比较「有/无」，既避免泄露又不依赖其内容。
func pushDisabledReasonKey(c Config) string {
	return strings.Join([]string{
		pushDisabledReason(c),
		normalizeAddress(c.Address),
		tokenPresence(c.Token),
		strconv.FormatBool(c.EnablePush),
		strconv.Itoa(len(cleanTasks(c.Tasks))),
	}, "\x00")
}

// permSummary 把 Token 权限汇成一行中文，用于日志与自检输出。
func permSummary(t *TokenInfo) string {
	if t == nil {
		return "未知"
	}
	yn := func(b bool) string {
		if b {
			return "有"
		}
		return "无"
	}
	return fmt.Sprintf("列目录=%s 回收站删除=%s 永久删除=%s 消息推送=%s",
		yn(t.AllowList), yn(t.AllowDelete), yn(t.AllowDeletePermanently), yn(t.AllowPushMessage))
}

// probeCD2Status 主动探测一次 CD2（TCP + TokenInfo）并写入运行状态。
// 一次探测 = 1 次 TCP 连通性 + 1 次 TokenInfo，不构成任何「定时扫全树」循环。
func probeCD2Status(ctx context.Context, c Config) (*TokenInfo, error) {
	client, token, err := connectPushClient(ctx, c)
	if err != nil {
		setStatus("未连接: "+formatCD2Error(err).Error(), nil)
		return nil, err
	}
	client.Close()
	setStatus("已连接: "+token.RootDir, token)
	return token, nil
}

// markPushDeniedConnected 在 Token 缺少消息推送权限时，依然写入「已连接」状态与 Token，
// 使连接状态与权限徽章自动点亮（无需手点「测试连接」）。抽成独立函数便于单元测试。
func markPushDeniedConnected(token *TokenInfo) {
	if token == nil {
		return
	}
	setStatus("已连接: "+token.RootDir+"（Token 缺少消息推送权限）", token)
}

// statusMonitor 常驻自检 CD2 连接与 Token 权限，随进程生命周期存续。
//
// 首次连上采用指数退避（3s→6s→12s→24s→30s 封顶）；连上之后不退出，转为 30s 稳态刷新。
// 这样「CD2 比本容器启动得晚」或「CD2 中途重启」都能自动恢复，无需用户点任何按钮。
// 只做 TCPCheck + TokenInfo 这类轻量元数据调用，绝不遍历目录，
// 因此不属于「每 N 秒扫全树」，与风控约束不冲突。
// 仅在连接状态发生翻转时写日志（首次探测的结果也算一次翻转），避免每 30s 刷屏。
// ctx 取消即返回，不泄漏 goroutine。
func statusMonitor(ctx context.Context) {
	const (
		minBackoff   = 3 * time.Second
		maxBackoff   = 30 * time.Second
		steady       = 30 * time.Second // 连上后的稳态刷新间隔
		unconfigured = 15 * time.Second // 未配置地址 / Token 时的重试间隔
	)
	backoff := minBackoff
	connected := false // 是否已至少连上一次：决定用指数退避还是稳态间隔
	prevUp := false    // 上次探测是否连上：仅在翻转时记日志
	probed := false    // 是否已探测过：让「首次不可达」也留下一条可读日志

	var listenerKey string // 上次已记录的云端事件监听器状态签名
	var listenerCheckedAt time.Time

	for {
		if ctx.Err() != nil {
			return
		}
		c := currentConfig()
		if normalizeAddress(c.Address) == "" || strings.TrimSpace(c.Token) == "" {
			// 未配置：只更新状态并等待，绝不 return（用户随时可能补上配置）。
			setStatus("未配置 CD2 地址或 Token", nil)
			prevUp = false
			if !waitOrDone(ctx, unconfigured) {
				return
			}
			continue
		}

		token, err := probeCD2Status(ctx, c)
		up := err == nil
		flipped := up && !prevUp
		switch {
		case flipped: // 首次连上 或 由失败恢复正常
			appendLog("状态自检：CD2 连接正常，Token 根目录 %s；权限 %s", token.RootDir, permSummary(token))
		case !up && prevUp: // 由正常转为失败
			appendLog("状态自检：CD2 连接中断（%v），将持续重试", err)
		case !up && !probed: // 首次探测即失败
			appendLog("状态自检：CD2 暂不可达（%v），将持续重试", err)
		}
		if up {
			connected = true
			// 云端事件监听器状态：连接翻转时立即查一次，之后最多每 2 分钟一次并缓存。
			// 这是轻量元数据查询（不遍历目录），是「事件驱动忽然失效」的关键诊断信号。
			if flipped || listenerKey == "" || time.Since(listenerCheckedAt) >= cloudListenerInterval {
				listenerCheckedAt = time.Now()
				listenerKey = checkCloudEventListeners(ctx, c, listenerKey)
			}
		}
		prevUp = up
		probed = true

		wait := steady
		if !connected {
			wait = backoff
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
		if !waitOrDone(ctx, wait) {
			return
		}
	}
}

// cloudListenerInterval 是云端事件监听器状态的稳态复查间隔（2 分钟）。它是轻量元数据查询，
// 不遍历目录；配合「仅连接翻转时立即查一次」，既及时发现掉线又能压低请求频次。
const cloudListenerInterval = 2 * time.Minute

// offlineCompleted 判定「离线下载任务在本轮完成」：上一轮为 downloading、本轮已转为其它状态
// （finished / error / unknown）即视为「刚完成」——即便完成态是 error/unknown，也值得触发一次
// 扫描（目录里可能已落入部分文件，且清理前置检查会自行判断是否含未完成后缀）。
// 纯函数，便于表驱动单测。
func offlineCompleted(oldStatus, newStatus string) bool {
	return oldStatus == "downloading" && newStatus != "downloading"
}

// OfflineMonitorRuntime 描述「离线任务监控」的实时状态，供页面直接展示。
// 目的：回答用户最关心的问题——「离线监控到底有没有在跑？」此前该项在界面上毫无可观测性，
// 用户只能看到「没有日志」，无从判断它是「没生效」还是「在跑但恰好没有离线任务完成」。
type OfflineMonitorRuntime struct {
	Enabled         bool     `json:"enabled"`                   // 是否启用（间隔 > 0 且有清理目录）
	IntervalMinutes int      `json:"intervalMinutes"`           // 当前检查间隔（分钟）；0=关闭
	Since           string   `json:"since,omitempty"`           // 进入当前启用/关闭状态的时间
	LastCheckAt     string   `json:"lastCheckAt,omitempty"`     // 最近一轮状态查询的时间
	LastTriggerAt   string   `json:"lastTriggerAt,omitempty"`   // 最近一次因「离线完成」触发扫描的时间
	Triggers        int      `json:"triggers"`                  // 累计触发扫描次数
	Watching        []string `json:"watching,omitempty"`        // 最近一轮处于「下载中」的清理目录
	Note            string   `json:"note,omitempty"`            // 人类可读说明
}

var (
	offMonMu   sync.Mutex
	offMonStat = OfflineMonitorRuntime{Note: "未启动"}
)

// offlineMonSnapshot 返回离线监控状态的深拷贝快照（供 /api/state 与 /api/push 输出）。
func offlineMonSnapshot() OfflineMonitorRuntime {
	offMonMu.Lock()
	defer offMonMu.Unlock()
	s := offMonStat
	if offMonStat.Watching != nil {
		s.Watching = append([]string(nil), offMonStat.Watching...)
	}
	return s
}

// setOfflineMonState 更新离线监控的启用态与说明（自持锁）。
func setOfflineMonState(enabled bool, interval int, note string) {
	offMonMu.Lock()
	defer offMonMu.Unlock()
	if offMonStat.Enabled != enabled || offMonStat.IntervalMinutes != interval {
		offMonStat.Since = time.Now().Format("2006-01-02 15:04:05")
		// 关闭时清空「下载中」痕迹，避免陈旧数据误导。
		if !enabled {
			offMonStat.Watching = nil
		}
	}
	offMonStat.Enabled = enabled
	offMonStat.IntervalMinutes = interval
	offMonStat.Note = note
}

// markOfflineMonCheck 记录一轮检查结果（时间 + 当前下载中的目录）。
func markOfflineMonCheck(watching []string) {
	offMonMu.Lock()
	defer offMonMu.Unlock()
	offMonStat.LastCheckAt = time.Now().Format("2006-01-02 15:04:05")
	if watching == nil {
		watching = []string{}
	}
	offMonStat.Watching = watching
}

// markOfflineMonTrigger 记录一次因「离线完成」而触发的扫描。
func markOfflineMonTrigger() {
	offMonMu.Lock()
	defer offMonMu.Unlock()
	offMonStat.LastTriggerAt = time.Now().Format("2006-01-02 15:04:05")
	offMonStat.Triggers++
}

// offlineMonDisabledReason 给出「离线监控未运行」的可读原因。
func offlineMonDisabledReason(minutes, taskCount int) string {
	if minutes <= 0 {
		return "已关闭（间隔设为 0）"
	}
	if taskCount == 0 {
		return "无清理目录（请先在「连接 · 目录 · 规则」中添加目录）"
	}
	return "未启用"
}

// offlineMonitor 常驻监控清理目录的离线下载状态，检测「下载中→完成」翻转即触发一次扫描。
// 这是「离线任务监控」的核心循环：每个周期对每个清理目录做 1 次 ListOfflineFilesByPath
// 轻量状态查询（绝不遍历目录文件内容），仅在检测到完成翻转时触发扫描，故不构成「定时扫全树」。
//
// 可观测性（本轮新增）：启用/关闭、间隔变化、以及「下载中」目录集合的变化都会写日志；
// 运行态同步暴露到 /api/state 的 offlineMonitor 字段，页面据此显示「离线监控」当前状态。
//
// 周期用 waitOrDone 实现而非 time.Ticker：既满足 QA5 风控红线（生产文件仅 runEventFallbackScanner
// 允许 time.NewTicker），又语义等价（ctx 取消即热退出，不泄漏 goroutine）。
func offlineMonitor(ctx context.Context) {
	prev := map[string]string{} // path -> 上一轮 OfflineStatus.Status
	var lastSig string         // 上一轮「启用态 + 间隔 + 目录数」签名，仅变化时记日志
	var lastWatching string    // 上一轮「下载中」目录集合的签名，仅变化时记日志
	for {
		c := currentConfig()
		tasks := cleanTasks(c.Tasks)
		interval := time.Duration(c.OfflineMonitorMinutes) * time.Minute
		if len(tasks) == 0 || interval <= 0 {
			prev = map[string]string{}
			sig := fmt.Sprintf("off|tasks=%d|min=%d", len(tasks), c.OfflineMonitorMinutes)
			if sig != lastSig {
				lastSig = sig
				lastWatching = ""
				setOfflineMonState(false, c.OfflineMonitorMinutes, offlineMonDisabledReason(c.OfflineMonitorMinutes, len(tasks)))
				appendLog("离线任务监控：未运行（%s）", offlineMonDisabledReason(c.OfflineMonitorMinutes, len(tasks)))
			}
			if !waitOrDone(ctx, 60*time.Second) {
				return
			}
			continue
		}
		sig := fmt.Sprintf("on|tasks=%d|min=%d", len(tasks), c.OfflineMonitorMinutes)
		if sig != lastSig {
			lastSig = sig
			lastWatching = ""
			setOfflineMonState(true, c.OfflineMonitorMinutes, "运行中")
			appendLog("离线任务监控已启动：每 %d 分钟检查 %d 个清理目录的离线下载状态（目录：%s）",
				c.OfflineMonitorMinutes, len(tasks), strings.Join(tasks, "、"))
		}
		done, watching := offlineMonitorTick(ctx, c, tasks, prev)
		markOfflineMonCheck(watching)
		if w := strings.Join(watching, "、"); w != lastWatching {
			lastWatching = w
			if len(watching) > 0 {
				appendLog("离线任务监控：检测到 %d 个目录正在离线下载（%s），完成后将自动触发扫描", len(watching), w)
			} else {
				appendLog("离线任务监控：当前无进行中的离线下载，等待检测目录的离线状态变化")
			}
		}
		if len(done) > 0 {
			appendLog("离线任务监控：%d 个目录的离线下载已完成，触发扫描（%s）", len(done), strings.Join(done, "、"))
			markOfflineMonTrigger()
			// 与事件驱动相同的 panic 兜底：扫描期间的 panic 不拖垮本监控循环。
			go func() {
				defer func() {
					if r := recover(); r != nil {
						appendLog("离线监控触发扫描发生 panic：%v\n%s", r, debug.Stack())
					}
				}()
				// runScan 自带 scanBusy 互斥，进行中会返回「已有扫描任务」错误，忽略即可。
				if _, err := runScan(ctx, currentConfig().AllowDelete); err != nil {
					appendLog("离线监控触发扫描跳过：%v", err)
				}
			}()
		}
		if !waitOrDone(ctx, interval) {
			return
		}
	}
}

// offlineMonitorTick 查询一轮全部清理目录的离线状态，返回：
//   - done：本轮检测到「离线下载完成」（上一轮 downloading、本轮非 downloading）的目录路径；
//   - watching：本轮处于「下载中」的目录路径（供日志与页面展示「监控确实在盯哪些目录」）。
//
// CD2 不可达 / 未配置 / 查询超时一律静默降级（done=nil、watching=nil、不上抛错误）。
// prev 参数会被本函数原地更新为下一轮对比的基线。
func offlineMonitorTick(ctx context.Context, c Config, tasks []string, prev map[string]string) (done, watching []string) {
	client, err := newCD2Client(c)
	if err != nil {
		return nil, nil
	}
	defer client.Close()
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := client.TCPCheck(tctx); err != nil {
		return nil, nil
	}
	for _, t := range tasks {
		st := client.OfflineStatus(tctx, t)
		old := prev[t]
		if offlineCompleted(old, st.Status) {
			done = append(done, t)
		}
		if st.Status == "downloading" {
			watching = append(watching, t)
		}
		prev[t] = st.Status
	}
	return done, watching
}

// checkCloudEventListeners 查询各云盘的云端事件监听器状态（轻量元数据，不遍历目录），
// 写入 statusInfo.CloudAPIs，并在结果变化时记日志（isCloudEventListenerRunning=false 按证据分级提示，非绝对告警）。
// 返回本次结果签名（供调用方做「仅变化时记日志」的节流）。查询失败静默降级，绝不误报。
func checkCloudEventListeners(ctx context.Context, c Config, lastKey string) string {
	client, err := newCD2Client(c)
	if err != nil {
		return lastKey
	}
	defer client.Close()
	qctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	apis, err := client.CloudAPIs(qctx)
	if err != nil {
		return lastKey // 静默降级：拿不到时保持上一次结果，不误报（也不清空 statusInfo.CloudAPIs）
	}
	setCloudAPIs(apis)
	lines, key := cloudListenerReport(apis, pushHasLiveEvidence())
	if key == lastKey {
		return lastKey
	}
	for _, ln := range lines {
		appendLog("%s", ln)
	}
	return key
}

// cloudListenerReport 依据云盘列表生成「需要记录的日志行」与稳定签名 key。
// 纯函数，便于单测。pushLive 表示「本程序已确证仍在收到文件变更事件（FILE_SYSTEM_CHANGE）」
// （见 pushHasLiveEvidence；注意与「收到任意推送消息」区分——日志广播不算证据）：
//   - isCloudEventListenerRunning=true：运行中提示；
//   - false 且 pushLive：说明该标记并不代表推送会停，降级为提示，避免误报（实证场景）；
//   - false 且 !pushLive：如实说明「暂未收到该云盘的文件变更事件」，并指明本程序已由
//     「离线任务监控」与「事件静默兜底扫描」自动接替。
//
// 2026-09-18 文案更正：isCloudEventListenerRunning=false 是部分 CD2 版本的常态，
// 并非可修复的故障——旧文案「请到 CD2 检查该云盘连接/重新登录，或重启 CD2」是误导性的
// 排查建议（用户实测：该字段对所有云盘恒为 false，重启 CD2 也不改变，且用户明确禁止重启
// CD2 以免影响其它对接程序）。故此处改为中性「提示」，不再给出无效的排查动作。
//
// key 里含 pushLive，故「证据状态翻转」也会重记日志（否则只翻转证据时不会重新记录）。
func cloudListenerReport(apis []CloudAPI, pushLive bool) (lines []string, key string) {
	parts := make([]string, 0, len(apis)+1)
	for _, a := range apis {
		parts = append(parts, fmt.Sprintf("%s=%v", a.Name, a.IsCloudEventListenerRunning))
		if a.IsCloudEventListenerRunning {
			lines = append(lines, fmt.Sprintf("CD2 云盘「%s」云端事件通道已就绪（isCloudEventListenerRunning=true）", a.Name))
			continue
		}
		if pushLive {
			lines = append(lines, fmt.Sprintf(
				"提示：CD2 云盘「%s」未上报云端事件通道（isCloudEventListenerRunning=false）；但本程序已确证仍能收到该云盘的文件变更事件（FILE_SYSTEM_CHANGE），实时清理不受影响。",
				a.Name))
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"提示：CD2 云盘「%s」未上报云端事件通道（isCloudEventListenerRunning=false），本程序暂未收到该云盘的文件变更事件。这是部分 CD2 版本的常态、并非故障；实时清理由「离线任务监控」（默认每 1 分钟）与「事件静默兜底扫描」（默认每 15 分钟）自动接替（间隔可在页面调整），也可用「手动清理」立即处理。",
			a.Name))
	}
	parts = append(parts, fmt.Sprintf("pushLive=%v", pushLive))
	return lines, strings.Join(parts, ";")
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
