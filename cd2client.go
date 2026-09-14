package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// TokenInfo 是 GetApiTokenInfo 返回的令牌权限摘要。
type TokenInfo struct {
	RootDir                string `json:"rootDir"`
	FriendlyName           string `json:"friendlyName"`
	AllowList              bool   `json:"allowList"`
	AllowDelete            bool   `json:"allowDelete"`
	AllowDeletePermanently bool   `json:"allowDeletePermanently"`
	AllowPushMessage       bool   `json:"allowPushMessage"`
	ExpiresIn              uint64 `json:"expiresIn"`
}

// FileItem 是列表/扫描的最小文件表示。
type FileItem struct {
	Name                 string `json:"name"`
	Path                 string `json:"path"`
	DisplayPath          string `json:"displayPath"`
	Size                 int64  `json:"size"`
	IsDir                bool   `json:"isDir"`
	ReadOnly             bool   `json:"readOnly"`
	CanDeletePermanently bool   `json:"canDeletePermanently"`
	WriteTime            int64  `json:"writeTime"` // 秒级时间戳（writeTime.seconds），0 表示未知
}

// OfflineStat 是 ListOfflineFilesByPath 返回的状态。
type OfflineStat struct {
	Path   string `json:"path"`
	Status string `json:"status"` // unknown/finished/error/downloading
	Ready  bool   `json:"ready"`  // true=可扫描（无进行中离线任务）
	Note   string `json:"note,omitempty"`
}

// CD2Client 是 CD2 gRPC 的最小客户端（dynamicpb 动态解析 cd2.proto）。
type CD2Client struct {
	address      string
	token        string
	forceRefresh bool
	conn         *grpc.ClientConn
	resolver     *cd2Resolver
	marshaler    protojson.MarshalOptions
	rateLimiter  *Limiter
}

func newCD2Client(cfg Config) (*CD2Client, error) {
	cfg.Address = normalizeAddress(cfg.Address)
	if cfg.Address == "" {
		return nil, errors.New("CD2 gRPC 地址为空")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("CD2 Token 为空")
	}
	resolver, err := loadResolver()
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(cfg.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(dynamicCodec{})),
		// keepalive：CD2 重启或 TCP 半开时，长连的 PushMessage 流若不主动探活会永久阻塞
		//（stream.RecvMsg 既不返回 EOF 也不报错），订阅静默失效且再也不会重连。
		// 这里让客户端在 60s 无活动后 ping、再等 20s 无响应即判定连接死亡并重建，≈80s 内自愈。
		// PermitWithoutStream=false：仅在存在 active stream（如 PushMessage 长连）时才 ping，
		// 避免空闲期无脑 ping 触发 CD2 侧 grpc server 的 too_many_pings GOAWAY。
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                60 * time.Second,
			Timeout:             20 * time.Second,
			PermitWithoutStream: false,
		}),
	)
	if err != nil {
		return nil, err
	}
	return &CD2Client{
		address:      cfg.Address,
		token:        cfg.Token,
		forceRefresh: cfg.ForceRefresh,
		conn:         conn,
		resolver:     resolver,
		marshaler:    protojson.MarshalOptions{UseProtoNames: true},
		rateLimiter:  newLimiter(cfg.OpsPerSec, cfg.Burst),
	}, nil
}

func (c *CD2Client) Close() { _ = c.conn.Close() }

// wait 过令牌桶（5 ops/s，对齐 115 官方上限）。
func (c *CD2Client) wait(ctx context.Context) error {
	return c.rateLimiter.Wait(ctx)
}

func (c *CD2Client) TCPCheck(ctx context.Context) error {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", c.address)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (c *CD2Client) TokenInfo(ctx context.Context) (*TokenInfo, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	msg := dynamicpb.NewMessage(c.resolver.mustMsg("StringValue"))
	msg.Set(msg.Descriptor().Fields().ByName("value"), protoreflect.ValueOfString(c.token))
	out := dynamicpb.NewMessage(c.resolver.mustMsg("TokenInfo"))
	if err := c.conn.Invoke(ctx, "/clouddrive.CloudDriveFileSrv/GetApiTokenInfo", msg, out); err != nil {
		return nil, err
	}
	b, _ := c.marshaler.Marshal(out)
	return parseTokenInfo(b), nil
}

// parseTokenInfo 从 GetApiTokenInfo 响应的 protojson 字节解析 TokenInfo。
// 抽成独立函数，便于对「真实解析路径」做单元测试。
func parseTokenInfo(b []byte) *TokenInfo {
	var raw struct {
		RootDir      string `json:"rootDir"`
		FriendlyName string `json:"friendly_name"`
		ExpiresIn    string `json:"expires_in"`
		Permissions  struct {
			AllowList              bool `json:"allow_list"`
			AllowDelete            bool `json:"allow_delete"`
			AllowDeletePermanently bool `json:"allow_delete_permanently"`
			AllowPushMessage       bool `json:"allow_push_message"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		// 权限解析失败不再静默吞掉：字段漂移时（如 allow_push_message 字段号变化）会
		// 产出空权限且毫无痕迹，与历史 P1 是同类缺陷结构，此处至少留一条日志。
		appendLog("警告：Token 权限解析失败（%v），本次权限判定可能不准确", err)
	}
	exp, _ := strconv.ParseUint(raw.ExpiresIn, 10, 64)
	if raw.RootDir == "" {
		raw.RootDir = "/"
	}
	return &TokenInfo{
		RootDir:                raw.RootDir,
		FriendlyName:           raw.FriendlyName,
		AllowList:              raw.Permissions.AllowList,
		AllowDelete:            raw.Permissions.AllowDelete,
		AllowDeletePermanently: raw.Permissions.AllowDeletePermanently,
		AllowPushMessage:       raw.Permissions.AllowPushMessage,
		ExpiresIn:              exp,
	}
}

// List 返回指定路径下的直接子项（一次 GetSubFiles 调用）。
func (c *CD2Client) List(ctx context.Context, path string) ([]FileItem, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	msg := dynamicpb.NewMessage(c.resolver.mustMsg("ListSubFileRequest"))
	d := msg.Descriptor().Fields()
	msg.Set(d.ByName("path"), protoreflect.ValueOfString(normalizePath(path)))
	if f := d.ByName("forceRefresh"); f != nil {
		msg.Set(f, protoreflect.ValueOfBool(c.forceRefresh))
	}
	ctx = authCtx(ctx, c.token)
	stream, err := c.conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, "/clouddrive.CloudDriveFileSrv/GetSubFiles")
	if err != nil {
		return nil, err
	}
	if err := stream.SendMsg(msg); err != nil {
		return nil, err
	}
	if err := stream.CloseSend(); err != nil {
		return nil, err
	}
	items := []FileItem{}
	for {
		reply := dynamicpb.NewMessage(c.resolver.mustMsg("SubFilesReply"))
		err := stream.RecvMsg(reply)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		items = append(items, c.parseSubFiles(reply)...)
	}
	return items, nil
}

func (c *CD2Client) parseSubFiles(reply *dynamicpb.Message) []FileItem {
	field := reply.Descriptor().Fields().ByName("subFiles")
	list := reply.Get(field).List()
	out := make([]FileItem, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		m := list.Get(i).Message()
		fd := m.Descriptor().Fields()
		name := getString(m, fd.ByName("name"))
		path := getString(m, fd.ByName("fullPathName"))
		if path == "" {
			path = joinPath("/", name)
		}
		// proto3 非 optional 枚举无 presence：Directory(0) 与未设置无法区分，
		// 故不依赖 fileType，改以布尔字段为准。
		isDir := getBool(m, fd.ByName("isDirectory")) || getBool(m, fd.ByName("isCloudDirectory"))
		out = append(out, FileItem{
			Name:                 name,
			Path:                 normalizePath(path),
			Size:                 getInt64(m, fd.ByName("size")),
			IsDir:                isDir,
			ReadOnly:             getBool(m, fd.ByName("readOnly")),
			CanDeletePermanently: getBool(m, fd.ByName("canDeletePermanently")),
			WriteTime:            getTimestampSeconds(m, fd.ByName("writeTime")),
		})
	}
	return out
}

// OfflineStatus 直查目录的离线任务状态（P0-28 主防线）。
func (c *CD2Client) OfflineStatus(ctx context.Context, path string) OfflineStat {
	path = normalizePath(path)
	stat := OfflineStat{Path: path, Status: "unknown", Ready: true, Note: "离线状态不可读时按可扫描处理"}
	if err := c.wait(ctx); err != nil {
		stat.Note = formatCD2Error(err).Error()
		return stat
	}
	msg := dynamicpb.NewMessage(c.resolver.mustMsg("FileRequest"))
	fd := msg.Descriptor().Fields()
	msg.Set(fd.ByName("path"), protoreflect.ValueOfString(path))
	out := dynamicpb.NewMessage(c.resolver.mustMsg("OfflineFileListResult"))
	if err := c.conn.Invoke(authCtx(ctx, c.token), "/clouddrive.CloudDriveFileSrv/ListOfflineFilesByPath", msg, out); err != nil {
		stat.Note = formatCD2Error(err).Error()
		return stat
	}
	statusField := out.Descriptor().Fields().ByName("status")
	statusNum := out.Get(statusField).Enum()
	stat.Status = offlineStatusName(statusNum)
	stat.Ready = statusNum == 1 || statusNum == 0
	if stat.Ready {
		stat.Note = "离线任务已完成或该目录无进行中的离线任务"
	} else {
		stat.Note = "离线任务尚未完成，暂不扫描该目录"
	}
	return stat
}

// Delete 删除单文件。permanently=true 用 DeleteFilePermanently，否则 DeleteFile（进回收站）。
func (c *CD2Client) Delete(ctx context.Context, path string, permanently bool) error {
	if err := c.wait(ctx); err != nil {
		return err
	}
	msg := dynamicpb.NewMessage(c.resolver.mustMsg("FileRequest"))
	fd := msg.Descriptor().Fields()
	msg.Set(fd.ByName("path"), protoreflect.ValueOfString(normalizePath(path)))
	if f := fd.ByName("forceRefresh"); f != nil {
		msg.Set(f, protoreflect.ValueOfBool(c.forceRefresh))
	}
	out := dynamicpb.NewMessage(c.resolver.mustMsg("FileOperationResult"))
	method := "/clouddrive.CloudDriveFileSrv/DeleteFile"
	if permanently {
		method = "/clouddrive.CloudDriveFileSrv/DeleteFilePermanently"
	}
	if err := c.conn.Invoke(authCtx(ctx, c.token), method, msg, out); err != nil {
		return err
	}
	fd = out.Descriptor().Fields()
	if !getBool(out, fd.ByName("success")) {
		return errors.New(getString(out, fd.ByName("errorMessage")))
	}
	return nil
}

// SubscribePush 常驻订阅 PushMessage 流；每收到一条消息回调 onEvent(messageType)。
// 仅需解码字段1(messageType)；不占令牌桶（长连接，仅1次调用）。
func (c *CD2Client) SubscribePush(ctx context.Context, onEvent func(messageType int32)) error {
	req := dynamicpb.NewMessage(c.resolver.mustMsgFull("google.protobuf.Empty"))
	respDesc := c.resolver.mustMsg("CloudDrivePushMessage")
	ctx = authCtx(ctx, c.token)
	stream, err := c.conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, "/clouddrive.CloudDriveFileSrv/PushMessage")
	if err != nil {
		return err
	}
	if err := stream.SendMsg(req); err != nil {
		return err
	}
	if err := stream.CloseSend(); err != nil {
		return err
	}
	mtField := respDesc.Fields().ByName("messageType")
	for {
		msg := dynamicpb.NewMessage(respDesc)
		err := stream.RecvMsg(msg)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if onEvent == nil {
			continue
		}
		// messageType 在 proto 中声明为 int32（非 enum），故用 getInt64 读取后转 int32。
		// 注意：不能用 getEnumNumber——protoreflect.Value.Enum() 在非 enum 字段上会 panic。
		onEvent(int32(getInt64(msg, mtField)))
	}
}

func offlineStatusName(n protoreflect.EnumNumber) string {
	switch n {
	case 1:
		return "finished"
	case 2:
		return "error"
	case 3:
		return "downloading"
	default:
		return "unknown"
	}
}

// ---------- dynamicpb resolver ----------

type cd2Resolver struct{ files linker.Files }

func loadResolver() (*cd2Resolver, error) {
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{ImportPaths: []string{"."}}),
	}
	files, err := compiler.Compile(context.Background(), "cd2.proto")
	if err != nil {
		return nil, err
	}
	return &cd2Resolver{files: files}, nil
}

// mustMsg 按 clouddrive.<name> 解析消息描述符（自动补 clouddrive. 前缀）。
func (r *cd2Resolver) mustMsg(name string) protoreflect.MessageDescriptor {
	return r.mustMsgFull("clouddrive." + name)
}

// mustMsgFull 按完整名称（如 google.protobuf.Empty）解析消息描述符。
// 注意：protoreflect.FileDescriptor 接口没有 FindDescriptorByName，且导入文件不会自动
// 纳入查找范围，故这里手工递归遍历文件自身及其导入的声明表（含标准 import）。
func (r *cd2Resolver) mustMsgFull(fullName string) protoreflect.MessageDescriptor {
	lookup := protoreflect.FullName(fullName)
	for _, file := range r.files {
		if md := findMessageDescriptor(file, lookup); md != nil {
			return md
		}
	}
	panic("message descriptor not found: " + fullName)
}

// findMessageDescriptor 在文件自身及其（递归）导入中查找指定全名的消息描述符。
func findMessageDescriptor(file protoreflect.FileDescriptor, name protoreflect.FullName) protoreflect.MessageDescriptor {
	msgs := file.Messages()
	for i := 0; i < msgs.Len(); i++ {
		if m := findInMessage(msgs.Get(i), name); m != nil {
			return m
		}
	}
	imports := file.Imports()
	for i := 0; i < imports.Len(); i++ {
		if m := findMessageDescriptor(imports.Get(i).FileDescriptor, name); m != nil {
			return m
		}
	}
	return nil
}

// findInMessage 在消息及其嵌套消息中按全名查找。
func findInMessage(m protoreflect.MessageDescriptor, name protoreflect.FullName) protoreflect.MessageDescriptor {
	if m.FullName() == name {
		return m
	}
	nested := m.Messages()
	for i := 0; i < nested.Len(); i++ {
		if found := findInMessage(nested.Get(i), name); found != nil {
			return found
		}
	}
	return nil
}

type dynamicCodec struct{}

func (dynamicCodec) Marshal(v any) ([]byte, error) { return proto.Marshal(v.(proto.Message)) }
func (dynamicCodec) Unmarshal(data []byte, v any) error {
	return proto.Unmarshal(data, v.(proto.Message))
}
func (dynamicCodec) Name() string { return "proto" }

func authCtx(ctx context.Context, token string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
}

// ---------- dynamicpb getter helpers ----------

func getString(m protoreflect.Message, f protoreflect.FieldDescriptor) string {
	if f == nil || !m.Has(f) {
		return ""
	}
	return m.Get(f).String()
}

func getBool(m protoreflect.Message, f protoreflect.FieldDescriptor) bool {
	if f == nil || !m.Has(f) {
		return false
	}
	return m.Get(f).Bool()
}

func getInt64(m protoreflect.Message, f protoreflect.FieldDescriptor) int64 {
	if f == nil || !m.Has(f) {
		return 0
	}
	return m.Get(f).Int()
}

func getEnumNumber(m protoreflect.Message, f protoreflect.FieldDescriptor) protoreflect.EnumNumber {
	if f == nil || !m.Has(f) {
		return -1
	}
	return m.Get(f).Enum()
}

// getTimestampSeconds 从 google.protobuf.Timestamp 取出秒数；字段缺失/异常返回 0。
func getTimestampSeconds(m protoreflect.Message, f protoreflect.FieldDescriptor) int64 {
	if f == nil || !m.Has(f) {
		return 0
	}
	ts := m.Get(f).Message()
	if ts == nil {
		return 0
	}
	sf := ts.Descriptor().Fields().ByName("seconds")
	if sf == nil || !ts.Has(sf) {
		return 0
	}
	return ts.Get(sf).Int()
}

func formatCD2Error(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("CD2 连接或请求超时：请确认 gRPC 端口、防火墙和 Docker host 网络")
	}
	// 拨号失败是 *net.OpError（非 gRPC status），在 status.FromError 之前先识别。
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return fmt.Errorf("CD2 不可达：%v。请确认 CD2 已启动并已开启 gRPC 服务，地址端口正确（默认 127.0.0.1:19798），Docker 建议使用 host 网络", opErr)
	}
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unavailable:
			return fmt.Errorf("CD2 不可达：%s。请确认地址是 gRPC 端口，并建议 Docker 使用 host 网络", s.Message())
		case codes.Unauthenticated:
			return fmt.Errorf("CD2 Token 无效或过期：%s", s.Message())
		case codes.PermissionDenied:
			return fmt.Errorf("CD2 Token 权限不足：%s", s.Message())
		case codes.DeadlineExceeded:
			return fmt.Errorf("CD2 请求超时：%s", s.Message())
		}
	}
	return err
}
