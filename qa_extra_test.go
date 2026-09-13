package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- V3: shouldClean 边界 ----------

func TestQA_ShouldClean_EdgeCases(t *testing.T) {
	cfg := defaultConfig() // SizeLimitMB=20, AdExts=.txt,.html,.url,.lnk, VideoExts=.mp4,.mkv,.ts
	limit := int64(cfg.SizeLimitMB * 1024 * 1024)

	cases := []struct {
		name string
		item FileItem
		want bool
	}{
		{"empty suffix", FileItem{Name: "noext", Size: 10}, false},
		{"uppercase ad ext", FileItem{Name: "AD.URL", Size: 1}, true},
		{"uppercase video ext small", FileItem{Name: "SAMPLE.MP4", Size: 1024}, true},
		{"exactly at threshold", FileItem{Name: "exact.mp4", Size: limit}, true},
		{"one byte over threshold", FileItem{Name: "over.mp4", Size: limit + 1}, false},
		{"zero-size video", FileItem{Name: "empty.mp4", Size: 0}, false},
		{"ad ext with zero size", FileItem{Name: "ad.url", Size: 0}, true},
		{"unrelated docx", FileItem{Name: "keep.docx", Size: 10}, false},
	}
	for _, c := range cases {
		if got := shouldClean(c.item, cfg); got != c.want {
			t.Errorf("shouldClean(%s, size=%d)=%v want %v", c.name, c.item.Size, got, c.want)
		}
	}

	// 阈值被设为 0 时，视频规则整体失效（limit>0 守卫）。
	zero := cfg
	zero.SizeLimitMB = 0
	if shouldClean(FileItem{Name: "tiny.mp4", Size: 1}, zero) {
		t.Error("SizeLimitMB=0 should disable video matching")
	}

	// 负体积视频不应命中。
	if shouldClean(FileItem{Name: "neg.mp4", Size: -5}, cfg) {
		t.Error("negative-size video must not match")
	}
}

// ---------- V3: hasIncomplete 边界 ----------

func TestQA_HasIncomplete_EdgeCases(t *testing.T) {
	cfg := defaultConfig()

	// 空列表 → false
	if hasIncomplete(nil, cfg) {
		t.Error("empty list must return false")
	}
	if hasIncomplete([]FileItem{}, cfg) {
		t.Error("empty slice must return false")
	}

	// 未完成后缀只存在于「子目录」中（不在当层文件列表里）→ 不应判定。
	onlySubdir := []FileItem{
		{Name: "sub", IsDir: true},
		{Name: "a.txt"},
	}
	if hasIncomplete(onlySubdir, cfg) {
		t.Error("incomplete suffix living only in a subdirectory must not trigger")
	}

	// 当层直接含未完成文件 → true
	if !hasIncomplete([]FileItem{{Name: "a.txt"}, {Name: "movie.mp4.part"}}, cfg) {
		t.Error("direct incomplete file must trigger")
	}

	// 大小写混合后缀 → true
	if !hasIncomplete([]FileItem{{Name: "X.PART"}}, cfg) {
		t.Error("uppercase incomplete suffix must trigger")
	}

	// 目录项即便名字带未完成后缀也应被忽略（只看文件）。
	if hasIncomplete([]FileItem{{Name: "weird.!qB", IsDir: true}}, cfg) {
		t.Error("directory entries must be ignored by hasIncomplete")
	}
}

// ---------- V3: isFileSystemChange 边界 ----------

func TestQA_IsFileSystemChange_EdgeCases(t *testing.T) {
	if !isFileSystemChange(4) {
		t.Error("4 must be true")
	}
	for _, mt := range []int32{0, 1, 2, 3, 5, 8, -1, 1 << 30, 1<<31 - 1} {
		if isFileSystemChange(mt) {
			t.Errorf("messageType %d must be false", mt)
		}
	}
}

// ---------- V3: normalizeConfig 边界 ----------

func TestQA_NormalizeConfig_Clamps(t *testing.T) {
	// OpsPerSec: [0.5, 20]，越界回退默认 5
	for _, c := range []struct{ in, want float64 }{
		{0.4, 5}, {0.5, 0.5}, {5, 5}, {20, 20}, {20.1, 5}, {-3, 5},
	} {
		if got := normalizeConfig(Config{OpsPerSec: c.in}).OpsPerSec; got != c.want {
			t.Errorf("OpsPerSec(%v)=%v want %v", c.in, got, c.want)
		}
	}

	// Burst: [1, 50]，越界回退 10
	for _, c := range []struct{ in, want int }{
		{0, 10}, {1, 1}, {50, 50}, {51, 10}, {-1, 10},
	} {
		if got := normalizeConfig(Config{Burst: c.in}).Burst; got != c.want {
			t.Errorf("Burst(%d)=%d want %d", c.in, got, c.want)
		}
	}

	// MaxFilesPerRun: [1, 100000]，越界回退 2000
	for _, c := range []struct{ in, want int }{
		{0, 2000}, {1, 1}, {100000, 100000}, {100001, 2000},
	} {
		if got := normalizeConfig(Config{MaxFilesPerRun: c.in}).MaxFilesPerRun; got != c.want {
			t.Errorf("MaxFilesPerRun(%d)=%d want %d", c.in, got, c.want)
		}
	}

	// FileCooldownHours: [0, 168]，越界回退 6
	for _, c := range []struct{ in, want int }{
		{-1, 6}, {0, 0}, {168, 168}, {169, 6},
	} {
		if got := normalizeConfig(Config{FileCooldownHours: c.in}).FileCooldownHours; got != c.want {
			t.Errorf("FileCooldownHours(%d)=%d want %d", c.in, got, c.want)
		}
	}

	// PushDebounceSeconds: [1, 120]，越界回退 5（需求指定 0->5, 1->1, 120->120, 121->5）
	for _, c := range []struct{ in, want int }{
		{0, 5}, {1, 1}, {120, 120}, {121, 5}, {999, 5}, {-1, 5},
	} {
		if got := normalizeConfig(Config{PushDebounceSeconds: c.in}).PushDebounceSeconds; got != c.want {
			t.Errorf("PushDebounceSeconds(%d)=%d want %d", c.in, got, c.want)
		}
	}

	// MaxTotalBytes: [1, 1<<40]，越界回退 10<<30
	for _, c := range []struct {
		in, want int64
	}{
		{0, 10 << 30}, {1, 1}, {1 << 40, 1 << 40}, {(1 << 40) + 1, 10 << 30},
	} {
		if got := normalizeConfig(Config{MaxTotalBytes: c.in}).MaxTotalBytes; got != c.want {
			t.Errorf("MaxTotalBytes(%d)=%d want %d", c.in, got, c.want)
		}
	}
}

// ---------- V3: normalizeAddress 边界 ----------

func TestQA_NormalizeAddress_EdgeCases(t *testing.T) {
	cases := map[string]string{
		"":                          "",
		"192.168.1.5":               "192.168.1.5:19798",
		"192.168.1.5:19999":         "192.168.1.5:19999",
		"https://host.example":      "host.example:19798",
		"http://host:8000/":         "host:8000",
		"http://192.168.1.2:19798/": "192.168.1.2:19798",
		"  host:9000  ":             "host:9000",
		"[::1]:19798":               "[::1]:19798",
		"::1":                       "::1", // 含冒号 → 不再补端口（IPv6 裸地址不受支持）
	}
	for in, want := range cases {
		if got := normalizeAddress(in); got != want {
			t.Errorf("normalizeAddress(%q)=%q want %q", in, got, want)
		}
	}
}

// ---------- V3: normalizePath 边界 ----------

func TestQA_NormalizePath_EdgeCases(t *testing.T) {
	cases := map[string]string{
		"":          "/",
		"/":         "/",
		"a/b":       "/a/b",
		"\\a\\b":    "/a/b",
		"  /a/b/  ": "/a/b",
		"\\":        "/",
		"/电影/测试":    "/电影/测试",
		"电影":        "/电影",
	}
	for in, want := range cases {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q)=%q want %q", in, got, want)
		}
	}
}

// ---------- V3: pushConsumer 防抖 ----------

func TestQA_PushDebounce_CoalesceThenRetrigger(t *testing.T) {
	const debounce = 80 * time.Millisecond
	var count int32
	p := newPushConsumer(nil, debounce, func() { atomic.AddInt32(&count, 1) })
	p.log = func(string, ...any) {}

	// 连发 N 次 FSC 事件（总时长 < 防抖窗口），应只触发 1 次。
	for i := 0; i < 6; i++ {
		p.handle(4)
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // 窗口过去 + 余量
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Fatalf("coalesce: want 1 trigger, got %d", got)
	}

	// 非 FSC 事件不得触发。
	p.handle(0)
	p.handle(1)
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Fatalf("non-FSC must not trigger, got %d", got)
	}

	// 防抖窗口过后再发一次 → 应能再次触发（计数变 2）。
	p.handle(4)
	time.Sleep(300 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 2 {
		t.Fatalf("re-trigger after window: want 2, got %d", got)
	}
}

// ---------- V3: formatCD2Error 文档化 ----------

func TestQA_FormatCD2Error(t *testing.T) {
	if formatCD2Error(nil) != nil {
		t.Error("formatCD2Error(nil) must be nil")
	}
	got := formatCD2Error(context.DeadlineExceeded)
	if got == nil || got.Error() == "" {
		t.Fatal("DeadlineExceeded should map to a message")
	}
	// 期望是中文可读信息（设计目标）。
	const wantSub = "超时"
	if len(got.Error()) < len(wantSub) || !containsSub(got.Error(), wantSub) {
		t.Errorf("DeadlineExceeded message should contain %q, got %q", wantSub, got.Error())
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
