package main

import (
	"context"
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
	trigger  func()
	log      func(format string, args ...any)

	mu    sync.Mutex  // 保护 timer
	timer *time.Timer // 当前待触发的防抖定时器

	tmu  sync.Mutex     // 保护 seen
	seen map[int32]bool // 已见过的消息类型（用于首次诊断日志，避免重复刷屏）
}

// newPushConsumer 构造一个 pushConsumer。debounce <= 0 时回退到 5 秒。
func newPushConsumer(client *CD2Client, debounce time.Duration, trigger func()) *pushConsumer {
	if debounce <= 0 {
		debounce = 5 * time.Second
	}
	return &pushConsumer{
		client:   client,
		debounce: debounce,
		trigger:  trigger,
		log:      appendLog,
		seen:     map[int32]bool{},
	}
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
// 并发安全：多路事件到来时用互斥保护 timer。非 FSC 事件直接忽略。
func (p *pushConsumer) handle(messageType int32) {
	p.noteType(messageType)
	if !isFileSystemChange(messageType) {
		return
	}
	bumpPushEvent()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(p.debounce, func() {
		if p.trigger != nil {
			p.trigger()
		}
	})
}

// run 常驻订阅直到 ctx 取消。订阅断开后按固定退避重连（重连退避不是扫描循环，允许）。
// 严禁在此处做任何「每 N 秒扫全树」的定时任务。
func (p *pushConsumer) run(ctx context.Context) {
	const backoff = 10 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := p.client.SubscribePush(ctx, p.handle)
		if ctx.Err() != nil {
			return
		}
		if p.log != nil {
			p.log("PushMessage 订阅断开，%s 后重连: %v", backoff, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}
