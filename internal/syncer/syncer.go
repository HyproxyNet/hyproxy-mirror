package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hyproxy-mirror/internal/config"
	"hyproxy-mirror/internal/logx"
	"hyproxy-mirror/internal/storage"
)

var (
	stateMu       sync.RWMutex
	stateData     = make(map[string]RepoState)
	stateFilePath string
)

// RepoState 保存仓库的最新版本、ETag，以及可按需代理的资产来源。
type RepoState struct {
	Tag         string                 `json:"tag"`
	ETag        string                 `json:"etag,omitempty"`
	AssetStatus map[string]AssetStatus `json:"asset_status"`
}

type AssetStatus struct {
	SourceURL string `json:"source_url"`
}

var httpClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: time.Second,
	},
}

const userAgent = "hyproxy-mirror/1.0"

func LoadState(path string) error {
	stateFilePath = path
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			stateData = make(map[string]RepoState)
			return nil
		}
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(contents, &raw); err != nil {
		stateData = make(map[string]RepoState)
		return err
	}

	loaded := make(map[string]RepoState, len(raw))
	for key, data := range raw {
		var state RepoState
		if err := json.Unmarshal(data, &state); err == nil && assetsHaveSources(state.AssetStatus) {
			loaded[key] = state
			continue
		}

		// 旧状态仅保存完成标记，无法安全代理；丢弃 ETag 以在下一轮强制重新获取资产地址。
		var legacy struct {
			Tag string `json:"tag"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return fmt.Errorf("decode state for %s: %w", key, err)
		}
		loaded[key] = RepoState{Tag: legacy.Tag, AssetStatus: make(map[string]AssetStatus)}
	}
	stateData = loaded
	return nil
}

func assetsHaveSources(assets map[string]AssetStatus) bool {
	for _, asset := range assets {
		if asset.SourceURL == "" {
			return false
		}
	}
	return true
}

func SaveState() error {
	stateMu.RLock()
	data, err := json.MarshalIndent(stateData, "", "  ")
	stateMu.RUnlock()
	if err != nil {
		return err
	}
	tmp := stateFilePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, stateFilePath)
}

func RunScheduler(ctx context.Context, localStore *storage.LocalStorage) {
	doSyncAll(ctx, localStore)
	ticker := time.NewTicker(config.GetInterval())
	defer ticker.Stop()

	logx.Infof("调度器启动，周期：%s", config.GetInterval())
	for {
		select {
		case <-ctx.Done():
			logx.Infof("调度器退出")
			return
		case <-ticker.C:
			doSyncAll(ctx, localStore)
		}
	}
}

func doSyncAll(ctx context.Context, localStore *storage.LocalStorage) {
	for _, repo := range config.Cfg.Repositories {
		if ctx.Err() != nil {
			return
		}
		syncLatest(ctx, repo, repo.Owner+"/"+repo.Repo, localStore)
	}
}

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

func syncLatest(ctx context.Context, repo config.RepoConfig, key string, localStore *storage.LocalStorage) {
	apiURL := "https://api.github.com/repos/" + repo.Owner + "/" + repo.Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		logx.Errorf("%s 请求构造失败: %v", key, err)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

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

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		logx.Warnf("%s 解析 releases 失败: %v", key, err)
		return
	}

	assets := make(map[string]AssetStatus, len(release.Assets))
	for _, asset := range release.Assets {
		name, err := sanitizeAssetName(asset.Name)
		if err != nil {
			logx.Warnf("%s 跳过非法资产名 %q: %v", key, asset.Name, err)
			continue
		}
		if _, err := url.ParseRequestURI(asset.BrowserDownloadURL); err != nil {
			logx.Warnf("%s 跳过无效资产地址 %q", key, name)
			continue
		}
		assets[name] = AssetStatus{SourceURL: asset.BrowserDownloadURL}
	}

	updated := !hasOld || old.Tag != release.TagName
	stateMu.Lock()
	stateData[key] = RepoState{Tag: release.TagName, ETag: resp.Header.Get("ETag"), AssetStatus: assets}
	stateMu.Unlock()
	if err := SaveState(); err != nil {
		logx.Warnf("%s 保存状态失败: %v", key, err)
	}
	if updated {
		if err := localStore.RemoveRepo(repo.Path); err != nil {
			logx.Warnf("%s 删除旧缓存失败: %v", key, err)
		}
		logx.Infof("%s 检测到版本 %s，已清理旧缓存", key, release.TagName)
	} else {
		logx.Debugf("%s 已刷新资产清单", key)
	}
}

// ServeAsset 在缓存命中时本地提供内容；未命中时从源站流式转发给用户并同步写入缓存。
func ServeAsset(localStore *storage.LocalStorage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		objectKey, repo, name, asset, ok := lookupAsset(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if serveCached(w, r, localStore, objectKey, name) {
			return
		}

		unlock := localStore.Lock(objectKey)
		defer unlock()
		if serveCached(w, r, localStore, objectKey, name) {
			return
		}
		proxyAsset(w, r, localStore, objectKey, repo, name, asset)
	}
}

func serveCached(w http.ResponseWriter, r *http.Request, localStore *storage.LocalStorage, objectKey, name string) bool {
	file, err := os.Open(filepath.Join(localStore.BaseDir, objectKey))
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	http.ServeContent(w, r, name, info.ModTime(), file)
	return true
}

func lookupAsset(requestPath string) (string, string, string, AssetStatus, bool) {
	cleanPath := path.Clean("/" + strings.TrimPrefix(requestPath, "/"))
	stateMu.RLock()
	defer stateMu.RUnlock()
	for _, repo := range config.Cfg.Repositories {
		repoPath := "/" + strings.Trim(strings.ReplaceAll(repo.Path, "\\", "/"), "/")
		if !strings.HasPrefix(cleanPath, repoPath+"/") {
			continue
		}
		name, err := sanitizeAssetName(strings.TrimPrefix(cleanPath, repoPath+"/"))
		if err != nil {
			return "", "", "", AssetStatus{}, false
		}
		key := repo.Owner + "/" + repo.Repo
		state, exists := stateData[key]
		if !exists {
			return "", "", "", AssetStatus{}, false
		}
		asset, exists := state.AssetStatus[name]
		if !exists || asset.SourceURL == "" {
			return "", "", "", AssetStatus{}, false
		}
		return path.Join(strings.Trim(repo.Path, "/"), name), key, name, asset, true
	}
	return "", "", "", AssetStatus{}, false
}

func proxyAsset(w http.ResponseWriter, r *http.Request, localStore *storage.LocalStorage, objectKey, repo, name string, asset AssetStatus) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, asset.SourceURL, nil)
	if err != nil {
		http.Error(w, "invalid upstream URL", http.StatusBadGateway)
		return
	}
	req.Header.Set("User-Agent", userAgent)
	for _, header := range []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match"} {
		if value := r.Header.Get(header); value != "" {
			req.Header.Set(header, value)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, "upstream download failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, header := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if value := resp.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	w.Header().Set("Content-Disposition", mimeDisposition(name))
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return
	}

	var (
		cacheFile *os.File
		tempPath  string
	)
	// Range 响应只能代表资产的一部分，因此不写入缓存。
	if r.Header.Get("Range") == "" && resp.ContentLength >= 0 {
		cacheFile, tempPath, err = localStore.Prepare(objectKey, resp.ContentLength)
		if err != nil && !errors.Is(err, storage.ErrCacheDisabled) {
			logx.Warnf("%s 缓存准备失败: %v", name, err)
		}
	}
	if cacheFile != nil {
		defer func() {
			if cacheFile != nil {
				_ = cacheFile.Close()
			}
			if tempPath != "" {
				_ = os.Remove(tempPath)
			}
		}()
	}

	buffer := make([]byte, 256*1024)
	flusher, _ := w.(http.Flusher)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			if cacheFile != nil {
				if _, err := cacheFile.Write(buffer[:n]); err != nil {
					logx.Warnf("%s 缓存写入失败，继续代理: %v", name, err)
					cacheFile.Close()
					_ = os.Remove(tempPath)
					cacheFile, tempPath = nil, ""
				}
			}
			if _, err := w.Write(buffer[:n]); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			logx.Warnf("%s 上游流中断: %v", name, readErr)
			return
		}
	}

	if cacheFile == nil {
		return
	}
	if err := cacheFile.Sync(); err != nil {
		logx.Warnf("%s 缓存同步失败: %v", name, err)
		return
	}
	if err := cacheFile.Close(); err != nil {
		logx.Warnf("%s 缓存关闭失败: %v", name, err)
		return
	}
	cacheFile = nil
	if !assetStillCurrent(repo, name, asset.SourceURL) {
		return
	}
	if err := localStore.Commit(tempPath, objectKey); err != nil {
		logx.Warnf("%s 缓存提交失败: %v", name, err)
		return
	}
	tempPath = ""
}

func assetStillCurrent(repo, name, sourceURL string) bool {
	stateMu.RLock()
	defer stateMu.RUnlock()
	state, exists := stateData[repo]
	if !exists {
		return false
	}
	asset, exists := state.AssetStatus[name]
	return exists && asset.SourceURL == sourceURL
}

func mimeDisposition(name string) string {
	return "attachment; filename*=UTF-8''" + url.PathEscape(name)
}

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

func GetStatus() []RepoOutput {
	stateMu.RLock()
	defer stateMu.RUnlock()

	out := make([]RepoOutput, 0, len(config.Cfg.Repositories))
	for _, repo := range config.Cfg.Repositories {
		key := repo.Owner + "/" + repo.Repo
		result := RepoOutput{RepoName: key}
		if state, exists := stateData[key]; exists {
			result.TagName = state.Tag
			names := make([]string, 0, len(state.AssetStatus))
			for name := range state.AssetStatus {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				result.Assets = append(result.Assets, AssetOutput{
					AssetName: name,
					LocalPath: "/files/" + path.Join(strings.Trim(repo.Path, "/"), url.PathEscape(name)),
					Completed: true,
				})
			}
		}
		out = append(out, result)
	}
	return out
}
