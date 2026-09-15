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
	"unicode/utf8"

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

// PushEvent 是 PushMessage 中一条消息的最小可诊断负载。
//
// messageType 总是可信；Path/ChangeType/IsDir 是对 FILE_SYSTEM_CHANGE 负载的「尽力而为」提取
// （官方未给出 FileSystemChange 的字段号，故只能从 unknown fields 里推断，见 buildFileSystemChangeEvent）。
// ChangeOK/IsDirOK 标明对应值是否可信；Raw 是原始字段的紧凑摘要，仅用于诊断日志。
type PushEvent struct {
	Type       int32  `json:"type"`
	Path       string `json:"path,omitempty"`       // 尽力而为提取的变更路径（可能为空）
	ChangeType int32  `json:"changeType,omitempty"` // 尽力而为提取的变更类型（ChangeOK=true 时可信）
	ChangeOK   bool   `json:"-"`
	IsDir      bool   `json:"isDir,omitempty"`
	IsDirOK    bool   `json:"-"`
	Raw        string `json:"raw,omitempty"` // FILE_SYSTEM_CHANGE 原始字段紧凑摘要（诊断用）
}

// SubscribePush 常驻订阅 PushMessage 流；每收到一条消息回调 onEvent(ev)。
// ev 携带 messageType 与（尽力而为提取的）文件系统变更负载；不占令牌桶（长连接，仅 1 次调用）。
func (c *CD2Client) SubscribePush(ctx context.Context, onEvent func(ev PushEvent)) error {
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
		mt := int32(getInt64(msg, mtField))
		ev := PushEvent{Type: mt}
		if isFileSystemChange(mt) {
			// CloudDrivePushMessage 只声明了字段1，oneof data（含 fileSystemChange=5）落入 unknown fields，
			// 这里从原始字节里尽力提取路径/变更类型，供日志与诊断使用。
			ev = buildFileSystemChangeEvent(mt, msg.GetUnknown())
		}
		onEvent(ev)
	}
}

// buildFileSystemChangeEvent 从一条 FILE_SYSTEM_CHANGE 消息的 unknown fields 里构造 PushEvent。
// 绝不 panic：任何缺失/异常都安全降级为空 Path 与未置位的 OK 标志。
func buildFileSystemChangeEvent(messageType int32, unknown []byte) PushEvent {
	ev := PushEvent{Type: messageType}
	top := parseRawFields(unknown)
	// fileSystemChange 是 oneof 中字段号 5（wire type 2，嵌套消息）——字段号来自官方指南 5929。
	inner := nestedBytes(top, 5)
	ev.Raw = renderRawFields(top, parseRawFields(inner))
	innerFields := parseRawFields(inner)
	ev.Path = bestPathCandidate(innerFields)
	ev.ChangeType, ev.ChangeOK, ev.IsDir, ev.IsDirOK = guessChangeAndDir(innerFields)
	return ev
}

// nestedBytes 在已解析字段中查找指定字段号且 wire type 为 2 的字段，返回其长度限定字节。
func nestedBytes(fields []rawField, number int) []byte {
	for _, f := range fields {
		if f.Number == number && f.WireType == 2 {
			return f.Bytes
		}
	}
	return nil
}

// rawField 表示从 protobuf 原始字节（未知字段）解析出的一个字段。
type rawField struct {
	Number   int
	WireType int
	Bytes    []byte // wire type 2（length-delimited）
	Varint   uint64 // wire type 0
	Text     string // wire type 2 的 UTF-8 尽力解码（不可打印时为 ""）
}

// parseRawFields 以「绝不 panic、失败即安全退出」的方式解析 protobuf wire 字节。
// 解析到任何截断、越界或未知 wire type 时，返回已成功解析的前缀（可能为空）。
func parseRawFields(b []byte) []rawField {
	fields := []rawField{}
	i := 0
	for i < len(b) {
		tag, n := decodeVarint(b[i:])
		if n == 0 {
			return fields
		}
		i += n
		num := int(tag >> 3)
		wire := int(tag & 7)
		if num == 0 {
			return fields
		}
		switch wire {
		case 0: // varint
			v, m := decodeVarint(b[i:])
			if m == 0 {
				return fields
			}
			i += m
			fields = append(fields, rawField{Number: num, WireType: 0, Varint: v})
		case 2: // length-delimited
			l, m := decodeVarint(b[i:])
			if m == 0 {
				return fields
			}
			i += m
			if l > uint64(len(b)-i) {
				return fields
			}
			chunk := b[i : i+int(l)]
			i += int(l)
			fields = append(fields, rawField{Number: num, WireType: 2, Bytes: chunk, Text: printableText(chunk)})
		case 1: // 64-bit
			if len(b)-i < 8 {
				return fields
			}
			i += 8
			fields = append(fields, rawField{Number: num, WireType: 1})
		case 5: // 32-bit
			if len(b)-i < 4 {
				return fields
			}
			i += 4
			fields = append(fields, rawField{Number: num, WireType: 5})
		default:
			// 3/4（已废弃的 group）等：无法安全继续，停止解析。
			return fields
		}
	}
	return fields
}

// decodeVarint 解析一个 base-128 varint；截断或溢出返回 (0,0)。
func decodeVarint(b []byte) (uint64, int) {
	var x uint64
	var s uint
	for i := 0; i < len(b) && i < 10; i++ {
		c := b[i]
		if c < 0x80 {
			return x | uint64(c)<<s, i + 1
		}
		x |= uint64(c&0x7f) << s
		s += 7
	}
	return 0, 0
}

// printableText 返回可打印的 UTF-8 文本；非法字节或含控制字符时返回 ""。
func printableText(b []byte) string {
	if len(b) == 0 || !utf8.Valid(b) {
		return ""
	}
	s := string(b)
	for _, r := range s {
		if r < 0x20 && r != '\t' {
			return ""
		}
	}
	return s
}

// bestPathCandidate 从一组字段里挑出最像「路径」的 length-delimited 字符串；没有则返回 ""。
func bestPathCandidate(fields []rawField) string {
	best, bestScore := "", 0
	for _, f := range fields {
		if f.WireType != 2 || f.Text == "" {
			continue
		}
		if s := scorePathLike(f.Text); s > bestScore {
			best, bestScore = f.Text, s
		}
	}
	return best
}

// scorePathLike 给「像路径」的字符串打分：以 / 开头最像，含 /、含扩展名次之。
func scorePathLike(s string) int {
	score := 0
	if strings.HasPrefix(s, "/") {
		score += 4
	}
	if strings.Contains(s, "/") {
		score += 2
	}
	if strings.Contains(s, "\\") {
		score++
	}
	if i := strings.LastIndex(s, "."); i > 0 && i < len(s)-1 && !strings.ContainsAny(s[i+1:], " /\\") {
		score++
	}
	return score
}

// guessChangeAndDir 尽力而为地从 FileSystemChange 内层字段推断 changeType / isDirectory。
// 官方未给出字段号，故仅按「枚举值通常 ≤3、布尔值通常 0/1」的弱约束给出结论，并置 OK 标志；
// 无法区分时返回未知（零值 + OK=false）。下一轮拿到真实字段号后应改为显式解析。
func guessChangeAndDir(fields []rawField) (ct int32, ctOK bool, isDir bool, dirOK bool) {
	for _, f := range fields {
		if f.WireType != 0 {
			continue
		}
		if !ctOK && f.Varint <= 3 {
			ct, ctOK = int32(f.Varint), true
			continue
		}
		if !dirOK && (f.Varint == 0 || f.Varint == 1) {
			isDir, dirOK = f.Varint == 1, true
		}
	}
	return ct, ctOK, isDir, dirOK
}

// renderRawFields 生成原始字段的紧凑诊断摘要（顶层 + 内层）。
func renderRawFields(top, inner []rawField) string {
	var b strings.Builder
	b.WriteString("top{")
	for i, f := range top {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(rawFieldDesc(f))
	}
	b.WriteString("}")
	if len(inner) > 0 {
		b.WriteString(" inner{")
		for i, f := range inner {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(rawFieldDesc(f))
		}
		b.WriteString("}")
	}
	return b.String()
}

// rawFieldDesc 描述单个原始字段（字段号 / wire type / 长度或取值 / 可打印文本）。
func rawFieldDesc(f rawField) string {
	switch f.WireType {
	case 0:
		return fmt.Sprintf("field=%d wire=0 val=%d", f.Number, f.Varint)
	case 2:
		if f.Text != "" {
			return fmt.Sprintf("field=%d wire=2 len=%d text=%q", f.Number, len(f.Bytes), f.Text)
		}
		return fmt.Sprintf("field=%d wire=2 len=%d", f.Number, len(f.Bytes))
	default:
		return fmt.Sprintf("field=%d wire=%d", f.Number, f.WireType)
	}
}

// CloudAPI 是 GetAllCloudApis 返回的单个云盘连接摘要（仅保留本工具需要的字段）。
type CloudAPI struct {
	Name                        string `json:"name"`
	IsCloudEventListenerRunning bool   `json:"isCloudEventListenerRunning"`
}

// CloudAPIs 调用 GetAllCloudApis 返回各云盘连接摘要（含云端事件监听器是否运行）。
// 轻量元数据查询：不遍历目录，不构成「定时扫全树」。
func (c *CD2Client) CloudAPIs(ctx context.Context) ([]CloudAPI, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	req := dynamicpb.NewMessage(c.resolver.mustMsgFull("google.protobuf.Empty"))
	out := dynamicpb.NewMessage(c.resolver.mustMsg("CloudAPIList"))
	if err := c.conn.Invoke(authCtx(ctx, c.token), "/clouddrive.CloudDriveFileSrv/GetAllCloudApis", req, out); err != nil {
		return nil, err
	}
	return parseCloudAPIs(out), nil
}

// parseCloudAPIs 从 CloudAPIList 消息解析云盘摘要。抽成纯函数便于单测。
func parseCloudAPIs(out protoreflect.Message) []CloudAPI {
	field := out.Descriptor().Fields().ByName("apis")
	if field == nil {
		return nil
	}
	list := out.Get(field).List()
	apis := make([]CloudAPI, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		m := list.Get(i).Message()
		fd := m.Descriptor().Fields()
		apis = append(apis, CloudAPI{
			Name:                        getString(m, fd.ByName("name")),
			IsCloudEventListenerRunning: getBool(m, fd.ByName("isCloudEventListenerRunning")),
		})
	}
	return apis
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
		return fmt.Errorf("CD2 不可达：%v。请确认 CD2 已启动并已开启 gRPC 服务、地址端口正确；Docker 桥接部署需填宿主机内网 IP（容器内的 127.0.0.1 指向容器自身），或使用 host 网络 / host.docker.internal", opErr)
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
