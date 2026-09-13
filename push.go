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
	}
}

// handle 处理单条推送消息：仅当为文件系统变更时刷新防抖定时器。
// 并发安全：多路事件到来时用互斥保护 timer。非 FSC 事件直接忽略。
func (p *pushConsumer) handle(messageType int32) {
	if !isFileSystemChange(messageType) {
		return
	}
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
