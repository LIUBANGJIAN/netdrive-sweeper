package main

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestQA_ManualScanAndCleanLogsHitDisk 是「控制台精简」的永久回归测试。
//
// 背景：精简后「② 运行日志」成为唯一的结果视图——独立的扫描结果表格与清理记录
// 面板都被删除。因此「手动清理」这个动作必须把可读日志真正写到
// 磁盘（data/clean.log），否则用户将彻底失去对操作结果的可见性。这条链路一旦回归，
// 页面会静默变空，必须用测试锁死。
//
// 做法：把 configPath/recordsPath/logPath 重定向到 t.TempDir()，配置一个非空清理
// 目录（绕过「空目录不扫描」前置拦截），并把 CD2 地址指向必然不可达的 127.0.0.1:1，
// 使 handleScan/handleClean 在「写日志」之后的连接阶段失败。日志写入发生在
// runScan 之前，故与 CD2 是否可用、能否真正删除无关——这正是要证明的解耦点。
func TestQA_ManualScanAndCleanLogsHitDisk(t *testing.T) {
	defer withTempPaths(t)()
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}

	stateMu.Lock()
	cfg = Config{
		Address:           "127.0.0.1:1", // 必然不可达 → 连接阶段快速失败
		Token:             "qa-token",    // 非空，避免在入参校验阶段就被拒
		OpsPerSec:         5,
		Burst:             10,
		Tasks:             []string{"/电影"}, // 非空，绕过「空目录不扫描」前置拦截
		DeletePermanently: false,           // 回收站模式，用于断言「删除方式：回收站」
	}
	stateMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 手动清理（仅扫描，未开启删除总开关）
	scanRec := httptest.NewRecorder()
	handleScan(scanRec, httptest.NewRequest("GET", "/api/scan", nil).WithContext(ctx))
	if scanRec.Code == 200 {
		t.Fatalf("CD2 不可达时 handleScan 不应返回 200，body=%s", scanRec.Body.String())
	}

	// 手动清理（删除方式：回收站）
	cleanRec := httptest.NewRecorder()
	handleClean(cleanRec, httptest.NewRequest("POST", "/api/clean", nil).WithContext(ctx))
	if cleanRec.Code == 200 {
		t.Fatalf("CD2 不可达时 handleClean 不应返回 200，body=%s", cleanRec.Body.String())
	}

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取 clean.log 失败（手动操作日志未落盘）: %v", err)
	}
	logs := string(b)
	for _, want := range []string{
		"手动清理开始（仅扫描，未开启删除总开关）",
		"手动清理开始（删除方式：回收站）",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("clean.log 缺少 %q\n实际日志:\n%s", want, logs)
		}
	}
	// 顺序：仅扫描先于回收站清理写入。
	if strings.Index(logs, "手动清理开始（仅扫描，未开启删除总开关）") > strings.Index(logs, "手动清理开始（删除方式：回收站）") {
		t.Fatalf("日志顺序异常，应为先扫描后清理：\n%s", logs)
	}

	// /api/logs（日志页唯一数据源）必须回显同一份磁盘内容。
	logRec := httptest.NewRecorder()
	handleLogs(logRec, httptest.NewRequest("GET", "/api/logs", nil))
	if logRec.Code != 200 || !strings.Contains(logRec.Body.String(), "手动清理开始（删除方式：回收站）") {
		t.Fatalf("/api/logs 未回显手动清理日志：code=%d body=%s", logRec.Code, logRec.Body.String())
	}
}
