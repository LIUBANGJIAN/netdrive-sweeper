package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ScanResult 是扫描/清理的结果摘要。
type ScanResult struct {
	Checked      int           `json:"checked"`
	Matched      int           `json:"matched"`
	Deleted      int           `json:"deleted"`
	Skipped      int           `json:"skipped"`
	Errors       []string      `json:"errors"`
	Items        []FileItem    `json:"items"`
	OfflineTasks []OfflineStat `json:"offlineTasks"`
	DeleteMode   string        `json:"deleteMode"` // recycle / permanent
}

// DeleteRecord 是一次删除动作的审计记录。
type DeleteRecord struct {
	Time    time.Time `json:"time"`
	Path    string    `json:"path"`
	Display string    `json:"displayPath"`
	Size    int64     `json:"size"`
	Rule    string    `json:"rule"`
	Mode    string    `json:"mode"` // recycle / permanent
	Result  string    `json:"result"`
	Error   string    `json:"error,omitempty"`
}

// shouldExclude 判断目录名是否命中排除关键词。
func shouldExclude(name string, cfg Config) bool {
	name = strings.ToLower(name)
	for _, x := range splitCSV(cfg.ExcludeDirs) {
		if x != "" && strings.Contains(name, strings.ToLower(x)) {
			return true
		}
	}
	return false
}

// shouldClean 判定单文件是否命中清理规则。
// 后缀命中 → 直接命中；视频文件 → 体积 ≤ 阈值才命中。
func shouldClean(f FileItem, cfg Config) bool {
	ext := strings.ToLower(filepath.Ext(f.Name))
	if ext == "" {
		return false
	}
	if contains(splitCSV(cfg.AdExts), ext) {
		return true
	}
	if contains(splitCSV(cfg.VideoExts), ext) {
		limit := int64(cfg.SizeLimitMB * 1024 * 1024)
		return limit > 0 && f.Size > 0 && f.Size <= limit
	}
	return false
}

// hasIncomplete 判断目录项中是否含未完成下载后缀。
func hasIncomplete(files []FileItem, cfg Config) bool {
	for _, f := range files {
		if f.IsDir {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext != "" && contains(splitCSV(cfg.IncompleteSuffixes), ext) {
			return true
		}
	}
	return false
}

// isFresh 判断文件是否仍在冷却期内（writeTime 距今不足 FileCooldownHours）。
func isFresh(f FileItem, cfg Config) bool {
	if cfg.FileCooldownHours <= 0 || f.WriteTime <= 0 {
		return false
	}
	age := time.Since(time.Unix(f.WriteTime, 0))
	return age < time.Duration(cfg.FileCooldownHours)*time.Hour
}

// sweeper 是扫描 + 清理的执行单元。
type sweeper struct {
	client *CD2Client
	cfg    Config
	token  *TokenInfo
	log    func(format string, args ...any)
}

func newSweeper(client *CD2Client, cfg Config, token *TokenInfo) *sweeper {
	return &sweeper{client: client, cfg: cfg, token: token, log: appendLog}
}

// run 执行扫描（deleteMode=false 仅预览，=true 执行删除）。
func (s *sweeper) run(ctx context.Context, deleteMode bool) (*ScanResult, error) {
	res := &ScanResult{DeleteMode: s.deleteModeName(deleteMode)}
	// T7 裁决：空目录 = 不扫描任何目录。提前给出明确错误，
	// 避免「扫描显示成功、其实什么都没扫」的困惑。
	tasks := cleanTasks(s.cfg.Tasks)
	if len(tasks) == 0 {
		return nil, errors.New("未配置任何目录，请先在「连接与目录」中添加要清理的目录")
	}
	if deleteMode {
		if !s.cfg.AllowDelete {
			return nil, errors.New("删除总开关未开启：请先在页面启用「允许自动清理」")
		}
		if s.cfg.DeletePermanently && !s.token.AllowDeletePermanently {
			return nil, errors.New("Token 缺少 allow_delete_permanently 权限，无法永久删除；请改用回收站删除或调整 Token 权限")
		}
		if !s.cfg.DeletePermanently && !s.token.AllowDelete {
			return nil, errors.New("Token 缺少 allow_delete 权限，无法删除到回收站")
		}
	}

	for _, task := range tasks {
		off := s.client.OfflineStatus(ctx, task)
		offPath := displayPath(s.token, task)
		off.Path = offPath
		res.OfflineTasks = append(res.OfflineTasks, off)
		if s.cfg.OfflineOnly && !off.Ready {
			s.log("跳过未完成离线目录 %s: %s", offPath, off.Note)
			continue
		}
		if err := s.scanDir(ctx, task, 0, deleteMode, res); err != nil {
			res.Errors = append(res.Errors, offPath+": "+formatCD2Error(err).Error())
		}
	}

	s.log("扫描完成 checked=%d matched=%d deleted=%d skipped=%d errors=%d", res.Checked, res.Matched, res.Deleted, res.Skipped, len(res.Errors))
	return res, nil
}

func (s *sweeper) deleteModeName(deleteMode bool) string {
	if !deleteMode {
		return "preview"
	}
	if s.cfg.DeletePermanently {
		return "permanent"
	}
	return "recycle"
}

// scanDir 递归扫描单目录。
func (s *sweeper) scanDir(ctx context.Context, path string, depth int, deleteMode bool, res *ScanResult) error {
	if s.cfg.MaxDepth > 0 && depth > s.cfg.MaxDepth {
		return nil
	}
	files, err := s.client.List(ctx, path)
	if err != nil {
		return err
	}
	res.Checked += len(files)

	// 未完成文件保护（P0-23）：目录含未完成后缀 → 整目录跳过
	if hasIncomplete(files, s.cfg) {
		display := displayPath(s.token, path)
		s.log("跳过下载中目录 %s（含未完成后缀）", display)
		res.Skipped++
		return nil
	}

	for _, f := range files {
		if shouldExclude(f.Name, s.cfg) {
			continue
		}
		if f.IsDir {
			if err := s.scanDir(ctx, f.Path, depth+1, deleteMode, res); err != nil {
				res.Errors = append(res.Errors, displayPath(s.token, f.Path)+": "+formatCD2Error(err).Error())
			}
			continue
		}
		if !shouldClean(f, s.cfg) {
			continue
		}
		f.DisplayPath = displayPath(s.token, f.Path)
		res.Matched++
		res.Items = append(res.Items, f)

		if deleteMode {
			if isFresh(f, s.cfg) {
				s.log("跳过未冷却文件 %s（mtime 距今不足 %dh）", f.DisplayPath, s.cfg.FileCooldownHours)
				res.Skipped++
				recordDelete(f, s.cfg, "skip_fresh", "")
				continue
			}
			if err := s.deleteOne(ctx, f, res); err != nil {
				res.Errors = append(res.Errors, f.DisplayPath+": "+formatCD2Error(err).Error())
				recordDelete(f, s.cfg, "fail", err.Error())
			}
		}
	}
	return nil
}

func (s *sweeper) deleteOne(ctx context.Context, f FileItem, res *ScanResult) error {
	if f.Size < 0 {
		f.Size = 0
	}
	// 单次执行上限校验（保险丝）：这里按累计实时检查，超限即中止本轮
	if res.Deleted >= s.cfg.MaxFilesPerRun {
		return errors.New("达到单次执行文件数上限，已中止（剩余命中项未删除）")
	}
	if err := s.client.Delete(ctx, f.Path, s.cfg.DeletePermanently); err != nil {
		return err
	}
	res.Deleted++
	mode := "recycle"
	if s.cfg.DeletePermanently {
		mode = "permanent"
	}
	s.log("已删除 %s（%s, %d 字节）", f.DisplayPath, mode, f.Size)
	recordDelete(f, s.cfg, "ok", "")
	return nil
}

// recordDelete 追加一条删除审计记录到 records.jsonl。
func recordDelete(f FileItem, cfg Config, result, errMsg string) {
	rec := DeleteRecord{
		Time:    time.Now(),
		Path:    f.Path,
		Display: f.DisplayPath,
		Size:    f.Size,
		Rule:    "junk",
		Mode:    map[bool]string{true: "permanent", false: "recycle"}[cfg.DeletePermanently],
		Result:  result,
		Error:   errMsg,
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	if err := ensureDataDirs(); err != nil {
		return
	}
	b, _ := json.Marshal(rec)
	fp, err := os.OpenFile(recordsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	_, _ = fp.Write(append(b, '\n'))
	_ = fp.Close()
}
