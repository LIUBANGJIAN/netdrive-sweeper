package main

// qa6_addr_verify_test.go —— 第二层独立验证（针对 57a86e1：gRPC 地址引导修正 + 容器内地址防呆）。
//
// 与实现者的 web_addr_hint_test.go 相互独立：
//	(a) 旧误导文案确已从生产可渲染文案中消失；新 placeholder/help/hint 确实在 HTML 里且不破坏原始字符串。
//	(b) Go 版 isLoopbackAddr 跑一张与「JS 版 node 实跑」完全相同的期望表 —— 两侧都对齐同一张表即证明语义一致。
//	(c) （由 node 桩驱动 renderAddrHint 覆盖，见报告）。
//	(d) inContainerWith 注入式 stat + /api/state、/api/push 响应体确实含 inContainer 布尔字段。
//
// 会经 appendLog 或写盘的用例一律 withTempPaths 重定向，不污染 data/clean.log。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// qa6Info 是本文件自用的 os.FileInfo 最小桩（不依赖其它测试文件的同名桩）。
type qa6Info struct{}

func (qa6Info) Name() string       { return "" }
func (qa6Info) Size() int64        { return 0 }
func (qa6Info) Mode() os.FileMode  { return 0 }
func (qa6Info) ModTime() time.Time { return time.Time{} }
func (qa6Info) IsDir() bool        { return false }
func (qa6Info) Sys() any           { return nil }

// (b) Go 版 isLoopbackAddr 必须逐例对齐「与 JS 侧完全相同的期望表」。
// 表内容与 C:\...\x_hint_test.js 中的 TABLE 一字不差 —— 两侧同表、同期望即证明 JS↔Go 一致。
func TestQA6_IsLoopbackAddrGoMatchesSharedTable(t *testing.T) {
	table := []struct {
		in   string
		want bool
	}{
		{"127.0.0.1:19798", true},
		{"localhost:19798", true},
		{"::1", true},
		{"[::1]:19798", true},
		{"[::1]", true},
		{"0.0.0.0:19798", true},
		{"127.0.0.1", true},
		{"http://127.0.0.1:19798", true},
		{"LOCALHOST:19798", true},
		{"  127.0.0.1:19798  ", true},
		{"127.0.0.1:19798/path", true},
		{"localhost", true},
		{"0.0.0.0", true},
		{"192.168.10.252:19798", false},
		{"host.docker.internal:19798", false},
		{"192.168.10.252", false},
		{"", false},
		{"   ", false},
		{"fe80::1", false},
		{"[fe80::1]:19798", false},
		{"::1:19798", false},
		{"127.0.0.1:abc", false},
		{"127.0.0.1:", false},
		{"127.0.0.2:19798", false},
		{"[", false},
	}
	for _, c := range table {
		if got := isLoopbackAddr(c.in); got != c.want {
			t.Errorf("Go isLoopbackAddr(%q)=%v，期望 %v（与 JS 侧同表）", c.in, got, c.want)
		}
	}
}

// 结构一致性：Go 与 JS 判定集合必须完全相同（四个字面量 + 端口剥离的关键构造）。
func TestQA6_LoopbackSetParityStructural(t *testing.T) {
	// Go 侧：源码里应含四个回环字面量的 switch。
	srcGo := mustReadGoFile(t, "main.go")
	for _, lit := range []string{`"127.0.0.1"`, `"localhost"`, `"::1"`, `"0.0.0.0"`} {
		if !strings.Contains(srcGo, lit) {
			t.Fatalf("Go isLoopbackAddr 缺少回环字面量 %s", lit)
		}
	}
	// JS 侧：内联函数应含同一组字面量的返回表达式。
	jsExpr := `return host==='127.0.0.1'||host==='localhost'||host==='::1'||host==='0.0.0.0';`
	if !strings.Contains(pageHTML, jsExpr) {
		t.Fatal("JS isLoopbackAddr 的判定集合与 Go 版不一致（缺少或改写了回环字面量表达式）")
	}
}

// (d) inContainerWith：注入式 stat 决定性验证四种情形。
func TestQA6_InContainerWithInjectedStat(t *testing.T) {
	exist := func(paths ...string) func(string) (os.FileInfo, error) {
		set := map[string]bool{}
		for _, p := range paths {
			set[p] = true
		}
		return func(p string) (os.FileInfo, error) {
			if set[p] {
				return qa6Info{}, nil
			}
			return nil, os.ErrNotExist
		}
	}
	cases := []struct {
		name string
		stat func(string) (os.FileInfo, error)
		want bool
	}{
		{"仅 /.dockerenv", exist("/.dockerenv"), true},
		{"仅 /run/.containerenv", exist("/run/.containerenv"), true},
		{"两者都不存在", exist(), false},
		{"两者都存在", exist("/.dockerenv", "/run/.containerenv"), true},
	}
	for _, c := range cases {
		if got := inContainerWith(c.stat); got != c.want {
			t.Errorf("[%s] inContainerWith=%v，期望 %v", c.name, got, c.want)
		}
	}
}

// (d) 实际打请求：/api/state 与 /api/push 的 JSON 响应体必须含 inContainer *布尔* 字段，且与 inContainer() 一致。
func TestQA6_EndpointsExposeInContainerBool(t *testing.T) {
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
			t.Fatalf("[%s] code=%d body=%s", hc.name, rec.Code, rec.Body.String())
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("[%s] 响应解码失败: %v", hc.name, err)
		}
		raw, ok := m["inContainer"]
		if !ok {
			t.Fatalf("[%s] 响应缺少 inContainer 字段: %s", hc.name, rec.Body.String())
		}
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("[%s] inContainer 不是布尔 JSON: err=%v raw=%s", hc.name, err, raw)
		}
		if v != inContainer() {
			t.Fatalf("[%s] inContainer=%v 与后端 inContainer()=%v 不一致", hc.name, v, inContainer())
		}
	}
}

// (a) 旧误导文案必须从生产 HTML 中消失；新引导文案与提示元素必须在；内联 JS 无 {{（不破坏原始字符串）。
func TestQA6_WebAddrGuidanceFixed(t *testing.T) {
	// 旧句（把 127.0.0.1 当默认值）不得再出现在 pageHTML。
	if strings.Contains(pageHTML, "CD2 的 gRPC 端口，默认 127.0.0.1:19798") {
		t.Fatal("pageHTML 仍残留旧误导文案「CD2 的 gRPC 端口，默认 127.0.0.1:19798」")
	}
	// 新 placeholder / help 必须都在 HTML 中。
	must := []string{
		`placeholder="192.168.1.10:19798"`,
		"Docker 桥接部署：填群晖宿主机的内网 IP",
		"仅 host 网络模式或程序直接跑在宿主机上时才可用 127.0.0.1:19798",
		`id="addrHint"`,
		"当前程序运行在容器内（桥接网络）",
	}
	for _, m := range must {
		if !strings.Contains(pageHTML, m) {
			t.Fatalf("pageHTML 缺少新引导文案 %q", m)
		}
	}
	// 内联 <script> 区间不得出现 {{（Go 模板）——否则破坏原始字符串 / node --check。
	jsStart := strings.Index(pageHTML, "<script>")
	jsEnd := strings.LastIndex(pageHTML, "</script>")
	if jsStart < 0 || jsEnd < jsStart {
		t.Fatal("未找到内联 <script> 区间")
	}
	if strings.Contains(pageHTML[jsStart:jsEnd], "{{") {
		t.Fatal("内联 JS 出现 {{，会破坏 node --check")
	}
}
