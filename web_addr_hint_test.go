package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// ==================== 桥接模式 gRPC 地址防呆：后端纯函数 ====================

// TestIsLoopbackAddr 表驱动验证 isLoopbackAddr 的端口剥离与 IPv6 兼容。
// 这是「容器内 + 回环地址」防呆提示的判定核心，必须与前端内联 JS 版本语义保持一致。
func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"127.0.0.1:19798", true},
		{"localhost:19798", true},
		{"::1", true},
		{"[::1]:19798", true},
		{"0.0.0.0:19798", true},
		{"192.168.10.252:19798", false},
		{"host.docker.internal:19798", false},
		{"", false},
		// —— 边界强化：无端口、带 scheme、大小写、前后空白 ——
		{"127.0.0.1", true},
		{"http://127.0.0.1:19798", true},
		{"LOCALHOST:19798", true},
		{"  127.0.0.1:19798  ", true},
		{"192.168.10.252", false},
		{"   ", false},
	}
	for _, c := range cases {
		if got := isLoopbackAddr(c.in); got != c.want {
			t.Errorf("isLoopbackAddr(%q)=%v，期望 %v", c.in, got, c.want)
		}
	}
}

// fakeFileInfo 是 os.FileInfo 的最小桩，仅用于 inContainerWith 的注入式单测。
type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }

// TestInContainerWith_FakeStat 用注入的假 stat 确定性验证容器标记文件探测。
func TestInContainerWith_FakeStat(t *testing.T) {
	docker := func(p string) (os.FileInfo, error) {
		if p == "/.dockerenv" {
			return fakeFileInfo{}, nil
		}
		return nil, os.ErrNotExist
	}
	if !inContainerWith(docker) {
		t.Fatal("存在 /.dockerenv 时应判定为容器内")
	}

	podman := func(p string) (os.FileInfo, error) {
		if p == "/run/.containerenv" {
			return fakeFileInfo{}, nil
		}
		return nil, os.ErrNotExist
	}
	if !inContainerWith(podman) {
		t.Fatal("存在 /run/.containerenv 时应判定为容器内")
	}

	none := func(p string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	if inContainerWith(none) {
		t.Fatal("两个标记文件都不存在时应判定为非容器")
	}
}

// ==================== 端点：/api/state 与 /api/push 必须返回 inContainer ====================

func TestEndpointsIncludeInContainer(t *testing.T) {
	defer withTempPaths(t)()
	if err := mustLoadConfig(); err != nil {
		t.Fatalf("mustLoadConfig: %v", err)
	}
	handlers := []struct {
		name string
		url  string
		h    http.HandlerFunc
	}{
		{"state", "/api/state", handleState},
		{"push", "/api/push", handlePush},
	}
	for _, hc := range handlers {
		rec := httptest.NewRecorder()
		hc.h(rec, httptest.NewRequest("GET", hc.url, nil))
		if rec.Code != 200 {
			t.Fatalf("%s code=%d body=%s", hc.name, rec.Code, rec.Body.String())
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("%s 响应解码失败: %v", hc.name, err)
		}
		if _, ok := m["inContainer"]; !ok {
			t.Fatalf("%s 响应缺少 inContainer 字段: %s", hc.name, rec.Body.String())
		}
	}
}

// ==================== 前端：地址防呆标记 + 内联 JS 安全 ====================

func TestWebStaticMarkers_AddrHint(t *testing.T) {
	must := []string{
		"function isLoopbackAddr(",
		"function renderAddrHint(",
		"el('address').addEventListener('input',renderAddrHint)",
		// 修正后的引导文案与示例宿主 IP
		`placeholder="192.168.1.10:19798"`,
		"Docker 桥接部署：填群晖宿主机的内网 IP",
		"host.docker.internal:19798",
		// 黄色内联提示文案
		"当前程序运行在容器内（桥接网络）",
		"id=\"addrHint\"",
		// 后端字段在前端被消费
		"inContainer=(j.inContainer===true)",
	}
	for _, m := range must {
		if !strings.Contains(pageHTML, m) {
			t.Fatalf("pageHTML 缺少地址防呆标记 %q", m)
		}
	}

	// 旧误导文案（把 127.0.0.1 当作默认值）必须移除。
	if strings.Contains(pageHTML, "CD2 的 gRPC 端口，默认 127.0.0.1:19798") {
		t.Fatal("pageHTML 仍残留把 127.0.0.1 当作默认值的误导文案")
	}

	// 内联 JS 区间不得出现 {{（Go 模板）——否则会破坏 node --check 与页面原始字符串。
	jsStart := strings.Index(pageHTML, "<script>")
	jsEnd := strings.LastIndex(pageHTML, "</script>")
	if jsStart < 0 || jsEnd < jsStart {
		t.Fatal("未找到内联 <script> 区间")
	}
	if strings.Contains(pageHTML[jsStart:jsEnd], "{{") {
		t.Fatal("内联 JS 出现 {{，会破坏 node --check")
	}
}
