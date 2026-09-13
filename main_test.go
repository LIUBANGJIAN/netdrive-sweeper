package main

import (
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestNormalizeAddress(t *testing.T) {
	cases := map[string]string{
		"http://192.168.1.2:19798/": "192.168.1.2:19798",
		"192.168.1.2":               "192.168.1.2:19798",
		" 192.168.1.2:19798 ":       "192.168.1.2:19798",
	}
	for in, want := range cases {
		if got := normalizeAddress(in); got != want {
			t.Fatalf("normalizeAddress(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizePathAndDisplayPath(t *testing.T) {
	if got := normalizePath("电影/测试"); got != "/电影/测试" {
		t.Fatalf("normalizePath got %q", got)
	}
	token := &TokenInfo{RootDir: "/BON_115网盘"}
	if got := displayPath(token, "/电影"); got != "/BON_115网盘/电影" {
		t.Fatalf("displayPath got %q", got)
	}
}

func TestShouldClean(t *testing.T) {
	cfg := defaultConfig()
	if !shouldClean(FileItem{Name: "ad.url", Size: 1}, cfg) {
		t.Fatal("ad file should match")
	}
	if !shouldClean(FileItem{Name: "sample.mp4", Size: 1024}, cfg) {
		t.Fatal("small video should match")
	}
	if shouldClean(FileItem{Name: "movie.mp4", Size: 100 * 1024 * 1024}, cfg) {
		t.Fatal("large video should not match")
	}
	if !shouldClean(FileItem{Name: "note.txt", Size: 10}, cfg) {
		t.Fatal("txt should match by suffix")
	}
	if shouldClean(FileItem{Name: "keep.docx", Size: 10}, cfg) {
		t.Fatal("non-junk docx should not match")
	}
}

func TestOfflineStatusName(t *testing.T) {
	cases := map[protoreflect.EnumNumber]string{0: "unknown", 1: "finished", 2: "error", 3: "downloading", 99: "unknown"}
	for in, want := range cases {
		if got := offlineStatusName(in); got != want {
			t.Fatalf("offlineStatusName(%d)=%q want %q", in, got, want)
		}
	}
}

func TestHasIncomplete(t *testing.T) {
	cfg := defaultConfig()
	files := []FileItem{{Name: "a.!qB"}, {Name: "b.txt"}}
	if !hasIncomplete(files, cfg) {
		t.Fatal("should detect incomplete suffix")
	}
	if hasIncomplete([]FileItem{{Name: "a.txt"}, {Name: "b.avi"}}, cfg) {
		t.Fatal("should not detect incomplete")
	}
}

func TestNormalizeConfig(t *testing.T) {
	c := normalizeConfig(Config{OpsPerSec: 0.1, Burst: 0, MaxFilesPerRun: 0, FileCooldownHours: -1, PushDebounceSeconds: 0})
	if c.OpsPerSec != 5.0 {
		t.Fatalf("OpsPerSec should clamp to default 5, got %v", c.OpsPerSec)
	}
	if c.Burst != 10 {
		t.Fatalf("Burst should clamp to 10, got %v", c.Burst)
	}
	if c.MaxFilesPerRun != 2000 {
		t.Fatalf("MaxFilesPerRun should clamp to 2000, got %v", c.MaxFilesPerRun)
	}
	if c.FileCooldownHours != 6 {
		t.Fatalf("FileCooldownHours should clamp to 6, got %v", c.FileCooldownHours)
	}
	if c.PushDebounceSeconds != 5 {
		t.Fatalf("PushDebounceSeconds should clamp to default 5, got %v", c.PushDebounceSeconds)
	}
	if got := normalizeConfig(Config{PushDebounceSeconds: 999}).PushDebounceSeconds; got != 5 {
		t.Fatalf("PushDebounceSeconds=999 should clamp to 5, got %v", got)
	}
	if got := normalizeConfig(Config{PushDebounceSeconds: 30}).PushDebounceSeconds; got != 30 {
		t.Fatalf("PushDebounceSeconds=30 should stay 30, got %v", got)
	}
}

func TestIsFileSystemChange(t *testing.T) {
	if !isFileSystemChange(4) {
		t.Fatal("messageType 4 (FILE_SYSTEM_CHANGE) should be true")
	}
	for _, mt := range []int32{0, 1, 2, 3, 5} {
		if isFileSystemChange(mt) {
			t.Fatalf("messageType %d should be false", mt)
		}
	}
}

func TestPushDebounceCoalesce(t *testing.T) {
	var mu sync.Mutex
	count := 0
	p := newPushConsumer(nil, 60*time.Millisecond, func() {
		mu.Lock()
		count++
		mu.Unlock()
	})
	p.log = func(string, ...any) {}

	// 连发 5 次 FSC 事件（间隔 10ms），应被防抖合并为 1 次。
	for i := 0; i < 5; i++ {
		p.handle(4)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	got := count
	mu.Unlock()
	if got != 1 {
		t.Fatalf("debounce should coalesce 5 events into 1 trigger, got %d", got)
	}

	// 非 FSC 事件不应触发。
	p.handle(0)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	got = count
	mu.Unlock()
	if got != 1 {
		t.Fatalf("non-FSC message must not trigger, got %d", got)
	}
}

func TestResolverPushDescriptors(t *testing.T) {
	r, err := loadResolver()
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}
	// mustMsgFull 必须能按全名解析标准 import（google.protobuf.Empty）。
	empty := r.mustMsgFull("google.protobuf.Empty")
	if empty.FullName() != "google.protobuf.Empty" {
		t.Fatalf("unexpected descriptor: %s", empty.FullName())
	}
	// PushMessage 响应消息 + messageType 字段号必须为 1。
	msg := r.mustMsg("CloudDrivePushMessage")
	if msg.FullName() != "clouddrive.CloudDrivePushMessage" {
		t.Fatalf("unexpected descriptor: %s", msg.FullName())
	}
	f := msg.Fields().ByName("messageType")
	if f == nil || f.Number() != 1 {
		t.Fatalf("messageType field #1 missing on CloudDrivePushMessage")
	}
}

// TestTokenPermissionsHasPushField 断言 proto 声明了 allow_push_message = 41。
// 若缺失，TokenInfo() 解码时 field 41 会落入 unknown fields 并被 protojson 丢弃，
// 导致 AllowPushMessage 恒为 false，事件驱动实时清理的门禁永远无法通过。
func TestTokenPermissionsHasPushField(t *testing.T) {
	r, err := loadResolver()
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}
	f := r.mustMsg("TokenPermissions").Fields().ByName("allow_push_message")
	if f == nil {
		t.Fatal("TokenPermissions.allow_push_message (field 41) must be declared in cd2.proto")
	}
	if f.Number() != 41 {
		t.Fatalf("allow_push_message field number = %d, want 41", f.Number())
	}
}

// TestParseTokenInfoDecodesPushPermission 走真实解析路径 parseTokenInfo：
// 用 resolver 构造 TokenInfo（permissions.allow_push_message=true, allow_list=true），
// 以与生产一致的 protojson(UseProtoNames) 序列化后喂给 parseTokenInfo，断言两者都被解出。
// 该测试在缺少 FIX-1（proto 无字段 41）时会失败，加上后通过。
func TestParseTokenInfoDecodesPushPermission(t *testing.T) {
	r, err := loadResolver()
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}
	perm := dynamicpb.NewMessage(r.mustMsg("TokenPermissions"))
	pf := perm.Descriptor().Fields()
	pushField := pf.ByName("allow_push_message")
	if pushField == nil {
		t.Fatal("TokenPermissions.allow_push_message (field 41) must be declared in cd2.proto")
	}
	perm.Set(pushField, protoreflect.ValueOfBool(true))
	perm.Set(pf.ByName("allow_list"), protoreflect.ValueOfBool(true))

	info := dynamicpb.NewMessage(r.mustMsg("TokenInfo"))
	info.Set(info.Descriptor().Fields().ByName("permissions"), protoreflect.ValueOfMessage(perm))

	// 复刻生产序列化：c.marshaler = protojson.MarshalOptions{UseProtoNames: true}。
	b, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(info)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	got := parseTokenInfo(b)
	if !got.AllowPushMessage {
		t.Fatalf("AllowPushMessage should be true after decoding, got false; bytes=%s", b)
	}
	if !got.AllowList {
		t.Fatalf("AllowList should be true after decoding; bytes=%s", b)
	}
}

// TestNormalizeConfig_BackfillsIncompleteSuffixes 证明 FIX-A：
// Web 保存把 incomplete_suffixes 写空时，normalizeConfig 会回填默认保护列表，
// 从而不会静默废掉 P0-23「含未完成后缀则整目录跳过」这道保险丝。
func TestNormalizeConfig_BackfillsIncompleteSuffixes(t *testing.T) {
	c := normalizeConfig(Config{IncompleteSuffixes: ""})
	if c.IncompleteSuffixes == "" {
		t.Fatal("empty incomplete_suffixes must be backfilled with defaults")
	}
	if !containsSub(c.IncompleteSuffixes, ".part") || !containsSub(c.IncompleteSuffixes, ".!qB") {
		t.Fatalf("backfilled incomplete_suffixes %q must contain .part and .!qB", c.IncompleteSuffixes)
	}
	// 仅空白也应回填。
	if ws := normalizeConfig(Config{IncompleteSuffixes: "   "}); ws.IncompleteSuffixes == "" {
		t.Fatal("whitespace-only incomplete_suffixes must be backfilled")
	}
	// 显式自定义值不得被覆盖。
	if custom := normalizeConfig(Config{IncompleteSuffixes: ".custom"}); custom.IncompleteSuffixes != ".custom" {
		t.Fatalf("explicit value must be preserved, got %q", custom.IncompleteSuffixes)
	}
}
