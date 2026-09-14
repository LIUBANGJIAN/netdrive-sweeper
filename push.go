package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// fileSystemChangeType 是 CD2 PushMessage 中「文件系统变更」的事件类型值。
// 对齐官方 CloudDrivePushMessage.MessageType.FILE_SYSTEM_CHANGE = 4。
const fileSystemChangeType int32 = 4

// isFileSystemChange 判断推送消息类型是否为文件系统变更事件。
// 提取为纯函数便于单元测试。
func isFileSystemChange(messageType int32) bool {
	return messageType == fileSystemChangeType
}

// pushConsumer 常驻订阅 CD2 PushMessage 流，对文件系统变更事件做「防抖合并」，
// 静默 debounce 时长后触发一次 trigger（替代定时轮询的唯一合法实时感知方式）。
//
// 防抖语义：一串突发事件只会触发一次 trigger；trigger 在独立 goroutine 中执行，
// 绝不阻塞订阅流本身。
type pushConsumer struct {
	client   *CD2Client
	debounce time.Duration
	// gen 是订阅世代（由 runPushConsumer 注入）。仅用于把「重连计数 / 最近错误」等运行时
	// 观测量按世代守卫地写入 pushStat，避免过期世代污染新世代状态。测试直接构造时为 0。
	gen int64
	// maxDelay 是单次突发的最大延迟上限（= 6×debounce）。纯防抖下持续不断的事件流会让
	// trigger 永远无法触发（即「有时行有时不行」的根因），此上限保证突发在最坏情况下也能按时触发。
	maxDelay time.Duration
	trigger  func()
	log      func(format string, args ...any)

	mu           sync.Mutex  // 保护 timer / firstEventAt
	timer        *time.Timer // 当前待触发的防抖定时器
	firstEventAt time.Time   // 当前突发中首个事件的时间；零值表示无待触发突发

	tmu  sync.Mutex     // 保护 seen
	seen map[int32]bool // 已见过的消息类型（用于首次诊断日志，避免重复刷屏）

	dmu          sync.Mutex // 保护 dedup / fscProbeLeft
	dedupSec     string     // 最近一次记录的秒（同秒同路径去重用）
	dedupPath    string     // 最近一次记录的路径
	fscProbeLeft int        // 剩余可打印「原始字段」诊断的事件数（首帧探测）
}

// newPushConsumer 构造一个 pushConsumer。debounce <= 0 时回退到 5 秒。
func newPushConsumer(client *CD2Client, debounce time.Duration, trigger func()) *pushConsumer {
	if debounce <= 0 {
		debounce = 5 * time.Second
	}
	return &pushConsumer{
		client:       client,
		debounce:     debounce,
		maxDelay:     6 * debounce, // 默认 5s 防抖 → 30s 上限
		trigger:      trigger,
		log:          appendLog,
		seen:         map[int32]bool{},
		fscProbeLeft: 5, // 只对前 5 个 FSC 事件打印原始字段结构（拿到真实字段号后即可停用）
	}
}

// onEvent 是 PushMessage 的回调入口：先做「事件可诊断性」记录，再进入既有防抖逻辑。
// 说明：handle(messageType int32) 的签名与实现保持不变（既有测试与结构锚定依赖它），
// 带负载的 PushEvent 在本函数里被消费，handle 继续只负责防抖调度。
func (p *pushConsumer) onEvent(ev PushEvent) {
	// 任意类型的推送到达都是「订阅存活」的证据。
	markPushMessage(ev.Type)
	if isFileSystemChange(ev.Type) {
		p.logFileSystemChange(ev)
		markPushEventPath(ev.Path)
	}
	p.handle(ev.Type)
}

// logFileSystemChange 记录一条文件系统变更事件（带路径）。FSC 是核心信号，每条都记；
// 仅对「同一秒内同路径」的重复明细做抑制（首次照记），避免刷屏。前 N 条附带原始字段诊断。
func (p *pushConsumer) logFileSystemChange(ev PushEvent) {
	sec := time.Now().Format("2006-01-02 15:04:05")

	p.dmu.Lock()
	probe := p.fscProbeLeft > 0
	if probe {
		p.fscProbeLeft--
	}
	dup := ev.Path != "" && p.dedupSec == sec && p.dedupPath == ev.Path
	p.dedupSec, p.dedupPath = sec, ev.Path
	p.dmu.Unlock()

	if probe && p.log != nil {
		p.log("【诊断】FILE_SYSTEM_CHANGE 原始字段：%s", ev.Raw)
	}
	if p.log == nil {
		return
	}
	if dup {
		return
	}
	if ev.Path == "" {
		p.log("收到文件系统变更事件（%s）：路径未知", changeTypeText(ev))
		return
	}
	p.log("收到文件系统变更事件（%s）：%s", changeTypeText(ev), ev.Path)
}

// changeTypeText 渲染变更类型描述。字段号未知故不做语义命名，仅在能提取到数值时给出原始值。
func changeTypeText(ev PushEvent) string {
	if !ev.ChangeOK {
		return "变更类型未知"
	}
	return fmt.Sprintf("changeType=%d", ev.ChangeType)
}

// pushTypeName 返回消息类型的中文名，仅用于日志诊断。
func pushTypeName(t int32) string {
	switch t {
	case 0:
		return "DOWNLOADER_COUNT"
	case 1:
		return "UPLOADER_COUNT"
	case 2:
		return "UPDATE_STATUS"
	case 3:
		return "FORCE_EXIT"
	case 4:
		return "FILE_SYSTEM_CHANGE"
	case 5:
		return "MOUNT_POINT_CHANGE"
	case 6:
		return "COPY_TASK_COUNT"
	case 7:
		return "LOG_MESSAGE"
	case 8:
		return "MERGE_TASKS"
	default:
		return "UNKNOWN"
	}
}

// noteType 记录「某消息类型首次到达」，只打一次日志。
// 目的：离线下载完成时若 CD2 未按预期发 FILE_SYSTEM_CHANGE=4，日志能立刻暴露真实的
// 类型值，便于把订阅范围对齐到实际事件，而不是继续盲猜。
func (p *pushConsumer) noteType(t int32) {
	p.tmu.Lock()
	first := !p.seen[t]
	p.seen[t] = true
	p.tmu.Unlock()
	if first && p.log != nil {
		p.log("PushMessage 事件到达 messageType=%d(%s)", t, pushTypeName(t))
	}
}

// handle 处理单条推送消息：仅当为文件系统变更时刷新防抖定时器。
// 并发安全：多路事件到来时用互斥保护 timer / firstEventAt。非 FSC 事件直接忽略。
//
// 防抖 + 最大延迟上限：突发中首个事件记录到 firstEventAt；只要突发尚未超过 maxDelay，
// 每次都按 debounce 顺延（合并抖动）。一旦达到 maxDelay 则立即触发，避免持续不断的事件流
// 把触发时刻无限推迟（纯 debounce 的「防抖饥饿」正是「有时行有时不行」的根因）。
func (p *pushConsumer) handle(messageType int32) {
	p.noteType(messageType)
	if !isFileSystemChange(messageType) {
		return
	}
	bumpPushEvent()

	// 关键：把「读取 firstEventAt 做决策」与「创建 / 取消定时器」放在同一次持锁内完成，
	// 消除「解锁后再 schedule」与上一次定时器的 fire() 交错（fire 清空 firstEventAt 并置空
	// timer、handle 又新建一个）导致同一突发事件多触发一轮扫描的竞态窗口。
	p.mu.Lock()
	if p.firstEventAt.IsZero() {
		p.firstEventAt = time.Now()
	}
	if time.Since(p.firstEventAt) >= p.maxDelay {
		// 达到最大延迟上限：锁内清空突发状态并停掉待触发定时器，解锁后立即异步触发。
		p.firstEventAt = time.Time{}
		if p.timer != nil {
			p.timer.Stop()
			p.timer = nil
		}
		p.mu.Unlock()
		p.asyncTrigger()
		return
	}
	// 未达上限：锁内按 debounce 顺延重建定时器。
	p.scheduleLocked(p.debounce)
	p.mu.Unlock()
}

// asyncTrigger 在独立 goroutine 中触发一次 trigger，绝不阻塞订阅流。
// 作为「立即触发」路径的公共出口（定时器回调路径仍走 fire，见其幂等守卫）。
func (p *pushConsumer) asyncTrigger() {
	go func() {
		if p.trigger != nil {
			p.trigger()
		}
	}()
}

// fire 取消待触发的定时器并立即触发一次 trigger。
// 锁内重置状态、解锁后在独立 goroutine 中调用 trigger——保持「trigger 不阻塞订阅流」的既有语义。
// 幂等：当已无待触发突发（例如达到上限的立即触发与末次事件的定时器同时到达）时不再重复触发。
func (p *pushConsumer) fire() {
	p.mu.Lock()
	active := !p.firstEventAt.IsZero() || p.timer != nil
	p.firstEventAt = time.Time{}
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.mu.Unlock()
	if !active {
		return
	}
	go func() {
		if p.trigger != nil {
			p.trigger()
		}
	}()
}

// scheduleLocked 以新的延迟重建防抖定时器（先停掉旧 timer）。
// 调用方必须已持有 p.mu —— 供 handle 在同一次持锁内完成「决策 + 重建定时器」。
func (p *pushConsumer) scheduleLocked(d time.Duration) {
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(d, p.fire)
}

// schedule 以新的延迟重建防抖定时器（取锁后转调 scheduleLocked）。
func (p *pushConsumer) schedule(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scheduleLocked(d)
}

// rearm 用于「本轮突发未真正被处理」（如扫描互斥）：把突发起点重置为现在并按 debounce 重新计时，
// 使这一轮稍后自动重试。目标：任何一次突发事件都不会因为扫描互斥而丢失。
func (p *pushConsumer) rearm() {
	p.mu.Lock()
	p.firstEventAt = time.Now()
	p.scheduleLocked(p.debounce)
	p.mu.Unlock()
}

// run 常驻订阅直到 ctx 取消。订阅断开后按「指数 + 抖动」退避重连（重连退避不是扫描循环，允许）。
// 建立/发起/结束/中断每次都有日志（含次数与世代），使「发起了几次、结束了几次、世代几」一眼可见。
// 重连计数与最近错误按世代守卫写入 pushStat，供前端展示（D5）。
// 严禁在此处做任何「每 N 秒扫全树」的定时任务。
func (p *pushConsumer) run(ctx context.Context) {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		attempt++
		if p.log != nil {
			p.log("正在建立 PushMessage 订阅（第 %d 次）", attempt)
			p.log("事件驱动订阅已发起（第 %d 次，世代 %d），等待 CD2 推送…", attempt, p.gen)
		}
		err := p.client.SubscribePush(ctx, p.onEvent)
		reason := pushExitReason(ctx, err)
		if p.log != nil {
			p.log("事件驱动订阅结束（第 %d 次，世代 %d，原因=%s）", attempt, p.gen, reason)
		}
		if err != nil {
			// 脱敏：formatCD2Error 的文本不含 Token 明文。
			markPushLastError(p.gen, formatCD2Error(err).Error())
		}
		if ctx.Err() != nil {
			return
		}
		delay := withReconnectJitter(reconnectBackoff(attempt-1), rand.Float64())
		if p.log != nil {
			if err == nil {
				p.log("PushMessage 订阅已结束（服务端关闭了订阅流，第 %d 次），%s 后重连", attempt, delay)
			} else {
				p.log("PushMessage 订阅中断（第 %d 次），%s 后重连：%v", attempt, delay, err)
			}
		}
		markPushReconnect(p.gen, attempt)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// pushExitReason 把一次订阅尝试的结束原因归类为可读文本：ctx 取消 / EOF / 错误。
// 用于让日志一眼看出「这次是优雅关闭还是中断」，避免把故障当成正常收尾。
func pushExitReason(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "ctx 取消"
	}
	if err == nil {
		return "EOF（服务端关闭订阅流）"
	}
	return "错误：" + err.Error()
}

const (
	reconnectBase = 2 * time.Second  // 首次重连基础退避
	reconnectMax  = 30 * time.Second // 退避封顶
)

// reconnectBackoff 返回第 attempt 次（0 起）重连的基础退避：2s→4s→8s→16s→30s 封顶。
// 纯函数，便于断言「单调不减且封顶」。
func reconnectBackoff(attempt int) time.Duration {
	d := reconnectBase
	for i := 0; i < attempt; i++ {
		if d >= reconnectMax {
			break
		}
		d *= 2
	}
	if d > reconnectMax {
		d = reconnectMax
	}
	return d
}

// withReconnectJitter 给退避叠加 [0.8, 1.0) 倍抖动（rnd ∈ [0,1)），避免多实例同时重连。
func withReconnectJitter(d time.Duration, rnd float64) time.Duration {
	f := 0.8 + 0.2*rnd
	return time.Duration(float64(d) * f)
}
