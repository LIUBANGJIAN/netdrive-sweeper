package main

// cd2_live_diag_test.go —— 独立诊断脚本（用真实 CD2 服务器 + API 信令实测）。
//
// 运行方式（不写死任何敏感信令，全部从环境变量注入）：
//   CD2_ADDR=192.168.10.252:19798 CD2_TOKEN=<信令> go test -run TestLiveDiagCD2 -v -count=1
//
// 诊断四件事（一次性回答「事件驱动为何失效」）：
//   1. 信令真实权限：GetApiTokenInfo 逐项摊开（allowPushMessage 是否真的 true）；
//   2. 每个云盘的云端事件监听器状态：GetAllCloudApis 逐云盘读 isCloudEventListenerRunning；
//   3. PushMessage 流里到底有没有文件变更事件（收到哪些 messageType 各几次、有无 FSC=4）；
//   4. 服务器版本（GetSystemInfo），判断是否版本过低导致监听器/推送行为差异。
//
// 目的：把「猜测」变成「事实」——云监听器未运行到底是 CD2 服务端状态、还是信令/网络问题，
// 由真实返回一次性裁决。不启动扫描、不删除任何文件、不调用 migrateConfig（故不污染 clean.log）。

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// TestLiveDiagCD2 是带真实 CD2 的诊断；未注入环境变量时立即 skip（不阻塞普通 CI）。
func TestLiveDiagCD2(t *testing.T) {
	addr := strings.TrimSpace(os.Getenv("CD2_ADDR"))
	token := strings.TrimSpace(os.Getenv("CD2_TOKEN"))
	if addr == "" || token == "" {
		t.Skip("未设置 CD2_ADDR / CD2_TOKEN，跳过真实诊断（不影响常规测试）")
	}

	cfg := Config{Address: addr, Token: token}
	client, err := newCD2Client(cfg)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	defer client.Close()
	ctx := context.Background()

	// —— 1. 信令真实权限 ——
	fmt.Println("========== 1. 信令权限 GetApiTokenInfo ==========")
	ti, err := client.TokenInfo(ctx)
	if err != nil {
		fmt.Printf("[错误] 读取信令信息失败: %v\n", formatCD2Error(err))
	} else {
		fmt.Printf("rootDir            = %s\n", ti.RootDir)
		fmt.Printf("friendlyName       = %s\n", ti.FriendlyName)
		fmt.Printf("allowList          = %v\n", ti.AllowList)
		fmt.Printf("allowDelete        = %v\n", ti.AllowDelete)
		fmt.Printf("allowDeletePermanent=%v\n", ti.AllowDeletePermanently)
		fmt.Printf("allowPushMessage   = %v\n", ti.AllowPushMessage)
		fmt.Printf("expiresIn          = %d 秒\n", ti.ExpiresIn)
	}

	// —— 2. 各云盘云端监听器状态 ——
	fmt.Println("\n========== 2. 云盘监听器状态 GetAllCloudApis ==========")
	apis, err := client.CloudAPIs(ctx)
	if err != nil {
		fmt.Printf("[错误] 读取云盘列表失败: %v\n", formatCD2Error(err))
	} else {
		if len(apis) == 0 {
			fmt.Println("返回 0 个云盘连接（可能信令 rootDir 受限，或 CD2 尚无已挂载云盘）")
		}
		for i, a := range apis {
			status := "运行中"
			if !a.IsCloudEventListenerRunning {
				status = "未运行 ← 事件驱动的关键故障点"
			}
			fmt.Printf("[%d] name=%q  isCloudEventListenerRunning=%v (%s)\n",
				i, a.Name, a.IsCloudEventListenerRunning, status)
		}
	}

	// —— 3. PushMessage 流实测（订阅 20 秒，统计各类消息计数，重点看有无 FSC=4）——
	fmt.Printf("\n========== 3. PushMessage 流实测（订阅 20 秒）==========\n")
	var mu sync.Mutex
	typeCounts := map[int32]int{}
	fscSamples := []string{}
	diagCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	done := make(chan error, 1)
	go func() {
		done <- client.SubscribePush(diagCtx, func(ev PushEvent) {
			mu.Lock()
			typeCounts[ev.Type]++
			if isFileSystemChange(ev.Type) && len(fscSamples) < 5 {
				fscSamples = append(fscSamples, fmt.Sprintf("type=%d path=%q changeType=%d raw=%s",
					ev.Type, ev.Path, ev.ChangeType, ev.Raw))
			}
			mu.Unlock()
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			fmt.Printf("[错误] PushMessage 订阅异常退出: %v\n", formatCD2Error(err))
		} else {
			fmt.Println("PushMessage 订阅正常结束（EOF）")
		}
	case <-diagCtx.Done():
		fmt.Println("（订阅观察窗口 20 秒结束）")
	}
	cancel()
	mu.Lock()
	if len(typeCounts) == 0 {
		fmt.Println("观察期内未收到任何推送消息")
	} else {
		fmt.Println("观察期内收到的 messageType 计数：")
		for mt, n := range typeCounts {
			fmt.Printf("  messageType=%d(%s) x %d\n", mt, pushTypeName(mt), n)
		}
		if typeCounts[fileSystemChangeType] > 0 {
			fmt.Println("→ 结论：收到 FILE_SYSTEM_CHANGE，事件通道是活的")
		} else {
			fmt.Println("→ 结论：未收到任何 FILE_SYSTEM_CHANGE（即使其它类型心跳存在）")
		}
	}
	for _, s := range fscSamples {
		fmt.Printf("  FSC 样例: %s\n", s)
	}
	mu.Unlock()

	// —— 4. 服务器版本 + 服务重启能力 ——
	fmt.Println("\n========== 4. 服务器版本 GetRuntimeInfo ==========")
	req := dynamicpb.NewMessage(client.resolver.mustMsgFull("google.protobuf.Empty"))
	out := dynamicpb.NewMessage(client.resolver.mustMsg("RuntimeInfo"))
	if err := client.conn.Invoke(authCtx(ctx, client.token), "/clouddrive.CloudDriveFileSrv/GetRuntimeInfo", req, out); err != nil {
		fmt.Printf("[错误] 读取运行时信息失败: %v\n", formatCD2Error(err))
	} else {
		fd := out.Descriptor().Fields()
		fmt.Printf("productName    = %s\n", getString(out, fd.ByName("productName")))
		fmt.Printf("productVersion = %s\n", getString(out, fd.ByName("productVersion")))
		fmt.Printf("cloudAPIVersion= %s\n", getString(out, fd.ByName("cloudAPIVersion")))
		fmt.Printf("osInfo         = %s\n", getString(out, fd.ByName("osInfo")))
	}

	fmt.Println("\n========== 5. 服务重启能力 GetServiceCapabilities ==========")
	capOut := dynamicpb.NewMessage(client.resolver.mustMsg("ServiceCapabilities"))
	if err := client.conn.Invoke(authCtx(ctx, client.token), "/clouddrive.CloudDriveFileSrv/GetServiceCapabilities", req, capOut); err != nil {
		fmt.Printf("[错误] 查询服务能力失败: %v\n", formatCD2Error(err))
	} else {
		fd := capOut.Descriptor().Fields()
		fmt.Printf("canRestart = %v\n", getBool(capOut, fd.ByName("canRestart")))
		fmt.Printf("canUpdate  = %v\n", getBool(capOut, fd.ByName("canUpdate")))
	}

	// —— 6. 离线任务结构探测 + 云盘完整字段 dump ——
	fmt.Println("\n========== 6. 云盘完整字段 dump（找 cloudAccountId 来源）==========")
	apiOut := dynamicpb.NewMessage(client.resolver.mustMsg("CloudAPIList"))
	if err := client.conn.Invoke(authCtx(ctx, client.token), "/clouddrive.CloudDriveFileSrv/GetAllCloudApis", req, apiOut); err != nil {
		fmt.Printf("[错误] 读取云盘列表失败: %v\n", formatCD2Error(err))
	} else {
		field := apiOut.Descriptor().Fields().ByName("apis")
		list := apiOut.Get(field).List()
		for i := 0; i < list.Len(); i++ {
			m := list.Get(i).Message()
			raw, _ := proto.Marshal(m.Interface())
			fmt.Printf("[cloudapi %d] wire=%s\n", i, renderRawFields(parseRawFields(raw), nil))
		}
	}

	fmt.Println("\n========== 7. 离线任务路径版探测 ListOfflineFilesByPath ==========")
	for _, p := range []string{"/BON_115网盘", "/BON_115网盘/私存入库"} {
		offReq2 := dynamicpb.NewMessage(client.resolver.mustMsg("FileRequest"))
		f2 := offReq2.Descriptor().Fields()
		offReq2.Set(f2.ByName("path"), protoreflect.ValueOfString(p))
		offOut2 := dynamicpb.NewMessage(client.resolver.mustMsg("OfflineFileListResult"))
		err := client.conn.Invoke(authCtx(ctx, client.token), "/clouddrive.CloudDriveFileSrv/ListOfflineFilesByPath", offReq2, offOut2)
		if err != nil {
			fmt.Printf("[%s] 失败: %v\n", p, formatCD2Error(err))
			continue
		}
		raw, _ := proto.Marshal(offOut2)
		fields := parseRawFields(raw)
		fmt.Printf("[%s] 原始 wire 结构：%s\n", p, renderRawFields(fields, nil))
		j, _ := client.marshaler.Marshal(offOut2)
		fmt.Printf("[%s] protojson 视图：%s\n", p, string(j))
	}
}