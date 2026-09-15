package main

import (
	"strings"
	"testing"
)

// TestWebUX_HideReadyLineAndUnifyChecks 回归用户本轮的三处前端调整：
//  1. 右上角不再显示「一切就绪…」与「建议：到②手动清理一次」；
//  2. 清理规则选项框不再出现红框（无 class="check warn"）；
//  3. 日志框改为随视口高度自适应（不再固定 max-height:420px）。
func TestWebUX_HideReadyLineAndUnifyChecks(t *testing.T) {
	mustNot := []string{
		"一切就绪，正在按规则运行",
		"建议：到「② 运行日志」点「手动清理」执行一次",
		`class="check warn"`,
		"max-height:420px",
	}
	for _, bad := range mustNot {
		if strings.Contains(pageHTML, bad) {
			t.Fatalf("pageHTML 不应再包含 %q", bad)
		}
	}

	must := []string{
		"height:clamp(280px,calc(100vh - 340px),1200px)",
		// 前端按「是否已确证仍在收推送」分级（后端 pushLive 证据位）。
		"pushLive=(lastPush.state==='running'",
		// 该筛选子串被 qa3 结构断言 pin 住，必须保留。
		"lastStatus.cloudApis.filter(function(a){return a.isCloudEventListenerRunning===false})",
		// 分级提示的两种文案（有证据 / 无证据）。
		"已确证仍能收到变更推送",
		"该标记仅表示 CD2 的云端原生推送通道未开启",
	}
	for _, m := range must {
		if !strings.Contains(pageHTML, m) {
			t.Fatalf("pageHTML 缺少 %q", m)
		}
	}
}

// TestCloudListenerKey_IncludesPushLive 证据位必须进入 key，否则「仅证据翻转」时不会重记日志。
func TestCloudListenerKey_IncludesPushLive(t *testing.T) {
	_, kNo := cloudListenerReport(nil, false)
	_, kLive := cloudListenerReport(nil, true)
	if kNo == kLive {
		t.Fatalf("pushLive 翻转后 key 必须不同，均为 %q", kNo)
	}
	_, kFalse := cloudListenerReport([]CloudAPI{{Name: "115", IsCloudEventListenerRunning: false}}, false)
	_, kTrue := cloudListenerReport([]CloudAPI{{Name: "115", IsCloudEventListenerRunning: false}}, true)
	if kFalse == kTrue {
		t.Fatalf("同盘同监听器状态、仅 pushLive 变化时 key 必须不同：%q", kFalse)
	}
}
