package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

const appName = "NetDrive Sweeper"

var (
	configPath = getenv("CONFIG_PATH", "data/config.json")
	cachePath  = getenv("CACHE_PATH", "data/cache.json")
	logPath    = getenv("LOG_PATH", "data/clean.log")
)

type Config struct {
	Address           string   `json:"address"`
	Token             string   `json:"token"`
	AdExts            string   `json:"ad_exts"`
	VideoExts         string   `json:"video_exts"`
	SizeLimitMB       float64  `json:"size_limit_mb"`
	ExcludeDirs       string   `json:"exclude_dirs"`
	ScanDelayMS       int      `json:"scan_delay_ms"`
	MaxDepth          int      `json:"max_depth"`
	ForceRefresh      bool     `json:"force_refresh"`
	DeletePermanently bool     `json:"delete_permanently"`
	OfflineOnly       bool     `json:"offline_only"`
	Tasks             []string `json:"tasks"`
}

type RuntimeStatus struct {
	Running     bool       `json:"running"`
	LastMessage string     `json:"last_message"`
	Token       *TokenInfo `json:"token,omitempty"`
}

type TokenInfo struct {
	RootDir                string `json:"rootDir"`
	FriendlyName           string `json:"friendlyName"`
	AllowList              bool   `json:"allowList"`
	AllowDelete            bool   `json:"allowDelete"`
	AllowDeletePermanently bool   `json:"allowDeletePermanently"`
	ExpiresIn              uint64 `json:"expiresIn"`
}

type FileItem struct {
	Name                 string `json:"name"`
	Path                 string `json:"path"`
	DisplayPath          string `json:"displayPath"`
	Size                 int64  `json:"size"`
	IsDir                bool   `json:"isDir"`
	ReadOnly             bool   `json:"readOnly"`
	CanDeletePermanently bool   `json:"canDeletePermanently"`
}

type OfflineTarget struct {
	Path        string `json:"path"`
	DisplayPath string `json:"displayPath"`
	Status      string `json:"status"`
	Ready       bool   `json:"ready"`
	Note        string `json:"note,omitempty"`
}

type ScanResult struct {
	Checked      int             `json:"checked"`
	Matched      int             `json:"matched"`
	Deleted      int             `json:"deleted"`
	Errors       []string        `json:"errors"`
	Items        []FileItem      `json:"items"`
	OfflineTasks []OfflineTarget `json:"offlineTasks"`
}

var (
	stateMu    sync.Mutex
	cfg        = defaultConfig()
	statusInfo = RuntimeStatus{LastMessage: "未连接"}
)

func main() {
	if err := mustLoadConfig(); err != nil {
		log.Printf("配置加载警告: %v，使用默认配置", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/state", handleState)
	mux.HandleFunc("/api/save", handleSave)
	mux.HandleFunc("/api/test", handleTest)
	mux.HandleFunc("/api/list", handleList)
	mux.HandleFunc("/api/scan", handleScan)
	mux.HandleFunc("/api/clean", handleClean)
	mux.HandleFunc("/api/logs", handleLogs)
	mux.HandleFunc("/api/clear_logs", handleClearLogs)

	addr := getenv("LISTEN", ":5000")
	log.Printf("%s 启动，监听 %s，配置文件: %s", appName, addr, configPath)
	if err := http.ListenAndServe(addr, logRequest(mux)); err != nil {
		log.Fatal(err)
	}
}

func defaultConfig() Config {
	return Config{
		Address:      "192.168.10.252:19798",
		AdExts:       ".txt,.html,.url,.lnk",
		VideoExts:    ".mp4,.mkv,.ts",
		SizeLimitMB:  5,
		ExcludeDirs:  "重要,备份",
		ScanDelayMS:  50,
		MaxDepth:     0,
		ForceRefresh: false,
		Tasks:        []string{"/"},
	}
}

func getenv(k, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return fallback
}

func mustLoadConfig() error {
	stateMu.Lock()
	defer stateMu.Unlock()
	if b, err := os.ReadFile(configPath); err == nil {
		b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("配置文件解析失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("配置文件读取失败: %w", err)
	}
	return ensureDataDirs()
}

func saveConfigLocked() error {
	if err := ensureDataDirs(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, b, 0600)
}

func ensureDataDirs() error {
	for _, p := range []string{configPath, cachePath, logPath} {
		if dir := filepath.Dir(p); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("创建目录失败 %s: %w", dir, err)
			}
		}
	}
	return nil
}

func currentConfig() Config {
	stateMu.Lock()
	defer stateMu.Unlock()
	return cfg
}

func setStatus(message string, token *TokenInfo) {
	stateMu.Lock()
	defer stateMu.Unlock()
	statusInfo.LastMessage = message
	if token != nil {
		statusInfo.Token = token
	}
}

func appendLog(format string, args ...any) {
	if err := ensureDataDirs(); err != nil {
		log.Printf("日志目录创建失败: %v", err)
		return
	}
	line := time.Now().Format("2006-01-02 15:04:05") + " " + fmt.Sprintf(format, args...) + "\n"
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
	log.Print(strings.TrimSpace(line))
}

func normalizeAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.Trim(raw, "/")
	if raw != "" && !strings.Contains(raw, ":") {
		raw += ":19798"
	}
	return raw
}

func normalizePath(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || raw == "/" {
		return "/"
	}
	return "/" + strings.Trim(raw, "/")
}

func displayPath(token *TokenInfo, apiPath string) string {
	root := "/"
	if token != nil && strings.TrimSpace(token.RootDir) != "" {
		root = strings.TrimRight(strings.ReplaceAll(token.RootDir, "\\", "/"), "/")
		if root == "" {
			root = "/"
		}
	}
	apiPath = normalizePath(apiPath)
	if apiPath == "/" {
		return root
	}
	return strings.TrimRight(root, "/") + apiPath
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	_ = pageTpl.Execute(w, map[string]any{"Title": appName})
}

func handleState(w http.ResponseWriter, r *http.Request) {
	stateMu.Lock()
	defer stateMu.Unlock()
	writeJSON(w, map[string]any{"config": cfg, "status": statusInfo})
}

func handleSave(w http.ResponseWriter, r *http.Request) {
	var next Config
	if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
		writeError(w, fmt.Errorf("配置解析失败: %w", err))
		return
	}
	next.Address = normalizeAddress(next.Address)
	next.Tasks = cleanTasks(next.Tasks)
	if len(next.Tasks) == 0 {
		next.Tasks = []string{"/"}
	}
	stateMu.Lock()
	cfg = next
	err := saveConfigLocked()
	stateMu.Unlock()
	if err != nil {
		writeError(w, fmt.Errorf("配置保存失败: %w", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true, "config": cfg})
}

func handleTest(w http.ResponseWriter, r *http.Request) {
	client, err := newCD2Client(currentConfig())
	if err != nil {
		writeError(w, err)
		return
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := client.TCPCheck(ctx); err != nil {
		writeError(w, formatCD2Error(err))
		return
	}
	info, err := client.TokenInfo(ctx)
	if err != nil {
		writeError(w, formatCD2Error(err))
		return
	}
	msg := "连接成功，Token 根目录: " + info.RootDir
	setStatus(msg, info)
	appendLog("✅ %s", msg)
	writeJSON(w, map[string]any{"ok": true, "token": info, "message": msg})
}

func handleList(w http.ResponseWriter, r *http.Request) {
	path := normalizePath(r.URL.Query().Get("path"))
	client, token, err := readyClient(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	defer client.Close()
	files, err := client.List(r.Context(), path)
	if err != nil {
		writeError(w, formatCD2Error(err))
		return
	}
	dirs := make([]FileItem, 0)
	for _, f := range files {
		if f.IsDir && !strings.HasPrefix(f.Name, ".") {
			dirs = append(dirs, f)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	for i := range dirs {
		dirs[i].DisplayPath = displayPath(token, dirs[i].Path)
	}
	writeJSON(w, map[string]any{"ok": true, "path": path, "displayPath": displayPath(token, path), "dirs": dirs, "token": token})
}

func handleScan(w http.ResponseWriter, r *http.Request) {
	res, err := runScan(r.Context(), false)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, res)
}

func handleClean(w http.ResponseWriter, r *http.Request) {
	res, err := runScan(r.Context(), true)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, res)
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	b, _ := os.ReadFile(logPath)
	writeJSON(w, map[string]string{"logs": string(b)})
}

func handleClearLogs(w http.ResponseWriter, r *http.Request) {
	ensureDataDirs()
	_ = os.WriteFile(logPath, nil, 0644)
	writeJSON(w, map[string]any{"ok": true})
}

func readyClient(ctx context.Context) (*CD2Client, *TokenInfo, error) {
	client, err := newCD2Client(currentConfig())
	if err != nil {
		return nil, nil, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := client.TCPCheck(checkCtx); err != nil {
		client.Close()
		return nil, nil, formatCD2Error(err)
	}
	token, err := client.TokenInfo(checkCtx)
	if err != nil {
		client.Close()
		return nil, nil, formatCD2Error(err)
	}
	if !token.AllowList {
		client.Close()
		return nil, nil, errors.New("Token 缺少 allow_list 权限，无法读取目录")
	}
	setStatus("Token 已读取: "+token.RootDir, token)
	return client, token, nil
}

func runScan(ctx context.Context, deleteMode bool) (*ScanResult, error) {
	client, token, err := readyClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	cfg := currentConfig()
	if deleteMode {
		if cfg.DeletePermanently && !token.AllowDeletePermanently {
			return nil, errors.New("Token 缺少 allow_delete_permanently 权限")
		}
		if !cfg.DeletePermanently && !token.AllowDelete {
			return nil, errors.New("Token 缺少 allow_delete 权限")
		}
	}
	res := &ScanResult{}
	for _, task := range cleanTasks(cfg.Tasks) {
		target := client.OfflineTarget(ctx, token, task)
		res.OfflineTasks = append(res.OfflineTasks, target)
		if cfg.OfflineOnly && !target.Ready {
			appendLog("跳过未完成离线目录 %s: %s", target.DisplayPath, target.Note)
			continue
		}
		scanPath(ctx, client, token, cfg, task, 0, deleteMode, res)
	}
	appendLog("扫描完成 checked=%d matched=%d deleted=%d errors=%d", res.Checked, res.Matched, res.Deleted, len(res.Errors))
	return res, nil
}

func scanPath(ctx context.Context, client *CD2Client, token *TokenInfo, cfg Config, path string, depth int, deleteMode bool, res *ScanResult) {
	if cfg.MaxDepth > 0 && depth > cfg.MaxDepth {
		return
	}
	files, err := client.List(ctx, path)
	if err != nil {
		res.Errors = append(res.Errors, displayPath(token, path)+": "+formatCD2Error(err).Error())
		return
	}
	for _, f := range files {
		res.Checked++
		if shouldExclude(f.Name, cfg) {
			continue
		}
		if f.IsDir {
			scanPath(ctx, client, token, cfg, f.Path, depth+1, deleteMode, res)
			continue
		}
		if shouldClean(f, cfg) {
			f.DisplayPath = displayPath(token, f.Path)
			res.Matched++
			res.Items = append(res.Items, f)
			if deleteMode {
				if err := client.Delete(ctx, f.Path, cfg.DeletePermanently); err != nil {
					res.Errors = append(res.Errors, f.DisplayPath+": "+formatCD2Error(err).Error())
				} else {
					res.Deleted++
					appendLog("🧹 已删除 %s", f.DisplayPath)
				}
			}
			if cfg.ScanDelayMS > 0 {
				time.Sleep(time.Duration(cfg.ScanDelayMS) * time.Millisecond)
			}
		}
	}
}

func shouldExclude(name string, cfg Config) bool {
	name = strings.ToLower(name)
	for _, x := range splitCSV(cfg.ExcludeDirs) {
		if x != "" && strings.Contains(name, strings.ToLower(x)) {
			return true
		}
	}
	return false
}

func shouldClean(f FileItem, cfg Config) bool {
	ext := strings.ToLower(filepath.Ext(f.Name))
	ad := contains(splitCSV(cfg.AdExts), ext)
	video := contains(splitCSV(cfg.VideoExts), ext)
	limit := int64(cfg.SizeLimitMB * 1024 * 1024)
	return ad || (video && limit > 0 && f.Size <= limit)
}

func splitCSV(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == ';' || r == '\n' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func contains(xs []string, v string) bool {
	v = strings.ToLower(v)
	for _, x := range xs {
		if strings.ToLower(x) == v {
			return true
		}
	}
	return false
}

func cleanTasks(tasks []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range tasks {
		t = normalizePath(t)
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

type CD2Client struct {
	cfg         Config
	conn        *grpc.ClientConn
	resolver    *CD2Resolver
	marshaler   protojson.MarshalOptions
	unmarshaler protojson.UnmarshalOptions
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
	conn, err := grpc.NewClient(cfg.Address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.ForceCodec(dynamicCodec{})))
	if err != nil {
		return nil, err
	}
	return &CD2Client{cfg: cfg, conn: conn, resolver: resolver, marshaler: protojson.MarshalOptions{UseProtoNames: true}, unmarshaler: protojson.UnmarshalOptions{DiscardUnknown: true}}, nil
}

func (c *CD2Client) Close() { _ = c.conn.Close() }

func (c *CD2Client) TCPCheck(ctx context.Context) error {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", c.cfg.Address)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (c *CD2Client) TokenInfo(ctx context.Context) (*TokenInfo, error) {
	msg := dynamicpb.NewMessage(c.resolver.mustMsg("StringValue"))
	msg.Set(msg.Descriptor().Fields().ByName("value"), protoreflect.ValueOfString(c.cfg.Token))
	out := dynamicpb.NewMessage(c.resolver.mustMsg("TokenInfo"))
	if err := c.conn.Invoke(ctx, "/clouddrive.CloudDriveFileSrv/GetApiTokenInfo", msg, out); err != nil {
		return nil, err
	}
	b, _ := c.marshaler.Marshal(out)
	var raw struct {
		RootDir      string `json:"rootDir"`
		FriendlyName string `json:"friendly_name"`
		ExpiresIn    string `json:"expires_in"`
		Permissions  struct {
			AllowList              bool `json:"allow_list"`
			AllowDelete            bool `json:"allow_delete"`
			AllowDeletePermanently bool `json:"allow_delete_permanently"`
		} `json:"permissions"`
	}
	_ = json.Unmarshal(b, &raw)
	exp, _ := strconv.ParseUint(raw.ExpiresIn, 10, 64)
	if raw.RootDir == "" {
		raw.RootDir = "/"
	}
	return &TokenInfo{RootDir: raw.RootDir, FriendlyName: raw.FriendlyName, AllowList: raw.Permissions.AllowList, AllowDelete: raw.Permissions.AllowDelete, AllowDeletePermanently: raw.Permissions.AllowDeletePermanently, ExpiresIn: exp}, nil
}

func (c *CD2Client) List(ctx context.Context, path string) ([]FileItem, error) {
	msg := dynamicpb.NewMessage(c.resolver.mustMsg("ListSubFileRequest"))
	d := msg.Descriptor().Fields()
	msg.Set(d.ByName("path"), protoreflect.ValueOfString(normalizePath(path)))
	msg.Set(d.ByName("forceRefresh"), protoreflect.ValueOfBool(c.cfg.ForceRefresh))
	stream, err := c.conn.NewStream(authCtx(ctx, c.cfg.Token), &grpc.StreamDesc{ServerStreams: true}, "/clouddrive.CloudDriveFileSrv/GetSubFiles")
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
		isDir := getBool(m, fd.ByName("isDirectory")) || getEnumNumber(m, fd.ByName("fileType")) == 0
		out = append(out, FileItem{Name: name, Path: normalizePath(path), Size: getInt64(m, fd.ByName("size")), IsDir: isDir, ReadOnly: getBool(m, fd.ByName("readOnly")), CanDeletePermanently: getBool(m, fd.ByName("canDeletePermanently"))})
	}
	return out
}

func (c *CD2Client) OfflineTarget(ctx context.Context, token *TokenInfo, path string) OfflineTarget {
	path = normalizePath(path)
	target := OfflineTarget{Path: path, DisplayPath: displayPath(token, path), Status: "unknown", Ready: true, Note: "未启用或无法读取离线状态时按目录扫描"}
	msg := dynamicpb.NewMessage(c.resolver.mustMsg("FileRequest"))
	fd := msg.Descriptor().Fields()
	msg.Set(fd.ByName("path"), protoreflect.ValueOfString(path))
	out := dynamicpb.NewMessage(c.resolver.mustMsg("OfflineFileListResult"))
	ctx, cancel := context.WithTimeout(authCtx(ctx, c.cfg.Token), 20*time.Second)
	defer cancel()
	if err := c.conn.Invoke(ctx, "/clouddrive.CloudDriveFileSrv/ListOfflineFilesByPath", msg, out); err != nil {
		target.Note = formatCD2Error(err).Error()
		return target
	}
	statusField := out.Descriptor().Fields().ByName("status")
	statusNum := out.Get(statusField).Enum()
	target.Status = offlineStatusName(statusNum)
	target.Ready = statusNum == 1 || statusNum == 0
	if target.Ready {
		target.Note = "离线任务已完成或该目录无进行中的离线任务"
	} else {
		target.Note = "离线任务尚未完成，暂不扫描该目录"
	}
	return target
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

func (c *CD2Client) Delete(ctx context.Context, path string, permanently bool) error {
	msg := dynamicpb.NewMessage(c.resolver.mustMsg("FileRequest"))
	fd := msg.Descriptor().Fields()
	msg.Set(fd.ByName("path"), protoreflect.ValueOfString(normalizePath(path)))
	if f := fd.ByName("forceRefresh"); f != nil {
		msg.Set(f, protoreflect.ValueOfBool(c.cfg.ForceRefresh))
	}
	out := dynamicpb.NewMessage(c.resolver.mustMsg("FileOperationResult"))
	method := "/clouddrive.CloudDriveFileSrv/DeleteFile"
	if permanently {
		method = "/clouddrive.CloudDriveFileSrv/DeleteFilePermanently"
	}
	if err := c.conn.Invoke(authCtx(ctx, c.cfg.Token), method, msg, out); err != nil {
		return err
	}
	fd = out.Descriptor().Fields()
	if !getBool(out, fd.ByName("success")) {
		return errors.New(getString(out, fd.ByName("errorMessage")))
	}
	return nil
}

type CD2Resolver struct{ files linker.Files }

func loadResolver() (*CD2Resolver, error) {
	compiler := protocompile.Compiler{Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{ImportPaths: []string{"."}})}
	files, err := compiler.Compile(context.Background(), "cd2.proto")
	if err != nil {
		return nil, err
	}
	return &CD2Resolver{files: files}, nil
}

func (r *CD2Resolver) mustMsg(name string) protoreflect.MessageDescriptor {
	fullName := protoreflect.FullName("clouddrive." + name)
	for _, file := range r.files {
		if fd := file.FindDescriptorByName(fullName); fd != nil {
			return fd.(protoreflect.MessageDescriptor)
		}
	}
	panic("message descriptor not found: " + name)
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

func joinPath(parent, name string) string {
	return normalizePath(strings.TrimRight(parent, "/") + "/" + strings.Trim(name, "/"))
}

func formatCD2Error(err error) error {
	if err == nil {
		return nil
	}
	if os.IsTimeout(err) || errors.Is(err, context.DeadlineExceeded) {
		return errors.New("CD2 连接或请求超时：请确认 gRPC 端口、防火墙和 Docker host 网络")
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
