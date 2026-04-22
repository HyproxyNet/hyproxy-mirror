package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hyproxy-mirror/internal/config"
	"hyproxy-mirror/internal/logx"
	"hyproxy-mirror/internal/storage"
)

// ---- 状态 ----

var (
	stateMu       sync.RWMutex
	stateData     = make(map[string]RepoState)
	stateFilePath string
)

// RepoState 保存仓库的最新 tag、ETag、资产完成状态
type RepoState struct {
	Tag         string          `json:"tag"`
	ETag        string          `json:"etag,omitempty"`
	AssetStatus map[string]bool `json:"asset_status"`
}

// ---- HTTP 客户端 ----

var httpClient = &http.Client{Timeout: 60 * time.Second}

const userAgent = "hyproxy-mirror/1.0"

// ---- 状态文件读写（原子） ----

func LoadState(path string) error {
	stateFilePath = path
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			stateData = make(map[string]RepoState)
			return nil
		}
		return err
	}
	var s map[string]RepoState
	if err := json.Unmarshal(b, &s); err != nil {
		stateData = make(map[string]RepoState)
		return err
	}
	stateData = s
	return nil
}

func SaveState() error {
	stateMu.RLock()
	defer stateMu.RUnlock()

	data, err := json.MarshalIndent(stateData, "", "  ")
	if err != nil {
		return err
	}
	tmp := stateFilePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, stateFilePath)
}

// ---- 调度器 ----

func RunScheduler(ctx context.Context, storage storage.Storage) {
	doSyncAll(ctx, storage)

	interval := config.GetInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logx.Infof("调度器启动，周期：%s", interval)

	for {
		select {
		case <-ctx.Done():
			logx.Infof("调度器退出")
			return
		case <-ticker.C:
			doSyncAll(ctx, storage)
		}
	}
}

func doSyncAll(ctx context.Context, storage storage.Storage) {
	for _, repo := range config.Cfg.Repositories {
		if ctx.Err() != nil {
			return
		}
		key := repo.Owner + "/" + repo.Repo
		syncLatest(ctx, repo, key, storage)
	}
}

// ---- 下载、清理 ----

func downloadFile(ctx context.Context, rawURL, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return io.ErrUnexpectedEOF
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	tmp := dest + ".dl.tmp"
	_ = os.Remove(tmp)

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(f, resp.Body)
	syncErr := f.Sync()
	closeErr := f.Close()

	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if syncErr != nil {
		_ = os.Remove(tmp)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}

	return os.Rename(tmp, dest)
}

func removeOldFiles(dir string, allowed map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if err := removeOldFiles(p, allowed); err != nil {
				return err
			}
			_ = os.Remove(p)
			continue
		}
		if !allowed[p] {
			_ = os.Remove(p)
		}
	}
	return nil
}

// ---- 同步主流程 ----

func sanitizeAssetName(name string) (string, error) {
	if name == "" {
		return "", errors.New("empty asset name")
	}
	name = filepath.Clean(name)
	if strings.Contains(name, "..") {
		return "", errors.New("invalid asset name")
	}
	name = filepath.Base(name)
	if name == "." || name == string(filepath.Separator) {
		return "", errors.New("invalid asset base")
	}
	return name, nil
}

func syncLatest(ctx context.Context, repo config.RepoConfig, key string, storage storage.Storage) {
	apiURL := "https://api.github.com/repos/" + repo.Owner + "/" + repo.Repo + "/releases/latest"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		logx.Errorf("%s 请求构造失败: %v", key, err)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	// 条件请求以节省流量
	stateMu.RLock()
	old, hasOld := stateData[key]
	stateMu.RUnlock()
	if hasOld && old.ETag != "" {
		req.Header.Set("If-None-Match", old.ETag)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		logx.Warnf("%s 请求失败: %v", key, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		logx.Debugf("%s 无更新(304)", key)
		return
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		logx.Warnf("%s GitHub API 状态码: %d", key, resp.StatusCode)
		return
	}

	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		logx.Warnf("%s 解析 releases 失败: %v", key, err)
		return
	}

	eTag := resp.Header.Get("ETag")
	localPath := filepath.Join(config.Cfg.Global.BaseDir, repo.Path)

	var status map[string]bool
	if !hasOld || old.Tag != rel.TagName {
		status = make(map[string]bool)
	} else {
		status = make(map[string]bool, len(old.AssetStatus))
		for k, v := range old.AssetStatus {
			status[k] = v
		}
	}

	for _, a := range rel.Assets {
		safeName, err := sanitizeAssetName(a.Name)
		if err != nil {
			logx.Warnf("%s 跳过非法资产名 %q: %v", key, a.Name, err)
			continue
		}
		if status[safeName] {
			continue
		}
		dest := filepath.Join(localPath, safeName)
		if err := downloadFile(ctx, a.BrowserDownloadURL, dest); err != nil {
			logx.Warnf("%s 下载失败 %s: %v", key, safeName, err)
			status[safeName] = false
		} else {
			status[safeName] = true
		}
	}

	stateMu.Lock()
	stateData[key] = RepoState{
		Tag:         rel.TagName,
		ETag:        eTag,
		AssetStatus: status,
	}
	stateMu.Unlock()
	if err := SaveState(); err != nil {
		logx.Warnf("%s 保存状态失败: %v", key, err)
	}

	allowed := make(map[string]bool)
	for name, ok := range status {
		if ok {
			allowed[filepath.Join(localPath, name)] = true
		}
	}
	if err := removeOldFiles(localPath, allowed); err != nil {
		logx.Warnf("%s 清理旧文件失败: %v", key, err)
	}

	logx.Infof("%s 已同步版本 %s", key, rel.TagName)
}

// ---- 对外状态 ----

type RepoOutput struct {
	RepoName string        `json:"RepoName"`
	TagName  string        `json:"TagName"`
	Assets   []AssetOutput `json:"Assets"`
}

type AssetOutput struct {
	AssetName string `json:"AssetName"`
	LocalPath string `json:"LocalPath"`
	Completed bool   `json:"Completed"`
}

// GetStatus 返回 UI/API 所需的汇总状态；LocalPath 使用 HTTP 路径语义
func GetStatus() []RepoOutput {
	stateMu.RLock()
	defer stateMu.RUnlock()

	var out []RepoOutput
	for _, repo := range config.Cfg.Repositories {
		key := repo.Owner + "/" + repo.Repo
		ro := RepoOutput{RepoName: key, TagName: "", Assets: nil}
		if st, ok := stateData[key]; ok {
			ro.TagName = st.Tag
			for name, done := range st.AssetStatus {
				ro.Assets = append(ro.Assets, AssetOutput{
					AssetName: name,
					LocalPath: "/files/" + path.Clean(strings.TrimPrefix(repo.Path, "/")) + "/" + url.PathEscape(name),
					Completed: done,
				})
			}
		}
		out = append(out, ro)
	}
	return out
}
