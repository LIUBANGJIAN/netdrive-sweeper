package main

import (
	"strings"
	"testing"
)

// TestWebUX_HideReadyLineAndUnifyChecks 回归本轮前端调整：
//  1. 右上角「下一步提示」整块移除（无 id="nextAction"、无 updateNextAction）；
//  2. 「一切就绪…」与「建议：到②手动清理一次」文案不再出现；
//  3. 清理规则选项框不再出现红框（无 class="check warn"）；
//  4. 日志框改为「全高工作区」：卡片撑满内容区、日志区随高度伸缩（不再固定 max-height:420px / 写死 calc(100vh - 340px)）；
//  5. 云端监听器 pushLive 改用与后端一致的时间窗判据（不再用粘性的累计 events）。
func TestWebUX_HideReadyLineAndUnifyChecks(t *testing.T) {
	mustNot := []string{
		`id="nextAction"`,
		"updateNextAction",
		"一切就绪，正在按规则运行",
		"建议：到「② 运行日志」点「手动清理」执行一次",
		`class="check warn"`,
		"max-height:420px",
		"(lastPush.events||0)>0",
		// 正常态（pushLive=true）的同义横幅已删除：不再逐盘声称「已确证仍能收到文件变更事件」
		// （pushLive 是全局信号，不支持逐盘断言；且该行只在一切正常时出现，属噪音）。
		"未上报云端事件通道，但本程序已确证",
	}
	for _, bad := range mustNot {
		if strings.Contains(pageHTML, bad) {
			t.Fatalf("pageHTML 不应再包含 %q", bad)
		}
	}

	must := []string{
		// 结构性重构：日志页改为「全高工作区」——日志容器 flex 撑满内容区剩余高度（替代旧写死 calc(100vh - 340px)）。
		"#tab-logs.tabpane.active{display:flex",
		".logbox{width:100%;height:auto;min-height:320px",
		// 前端 pushLive 与后端 pushEvidenceWindow 同口径的时间窗判据（2026-09-17 更正：
		// 证据改为 lastFileEventAt——LOG_MESSAGE=7 是 CD2 自身日志广播，不能证明文件事件通道存活）。
		"parseTS(lastPush.lastFileEventAt)",
		"<=600000",
		// 该筛选子串被 qa3 结构断言 pin 住，必须保留。
		"lastStatus.cloudApis.filter(function(a){return a.isCloudEventListenerRunning===false})",
		// 分级提示的两种文案（有文件事件证据 / 无证据）。
		"已确证仍能收到文件变更事件",
		"且最近未收到任何文件变更事件",
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
