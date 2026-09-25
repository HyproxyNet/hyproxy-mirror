package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

var ErrCacheDisabled = errors.New("cache disabled for this download")

// LocalStorage 管理本地下载缓存。文件在完整写入并同步后才会原子地出现在缓存中。
type LocalStorage struct {
	BaseDir string

	mu       sync.Mutex
	maxBytes int64

	flightMu sync.Mutex
	flights  map[string]*cacheFlight
}

type cacheFlight struct {
	mu   sync.Mutex
	refs int
}

func NewLocal(baseDir string, maxBytes int64) *LocalStorage {
	return &LocalStorage{BaseDir: baseDir, maxBytes: maxBytes, flights: make(map[string]*cacheFlight)}
}

// Lock 同一资产只允许一个上游拉取和缓存写入，避免并发请求互相覆盖临时文件。
func (ls *LocalStorage) Lock(objectKey string) func() {
	ls.flightMu.Lock()
	flight := ls.flights[objectKey]
	if flight == nil {
		flight = &cacheFlight{}
		ls.flights[objectKey] = flight
	}
	flight.refs++
	ls.flightMu.Unlock()

	flight.mu.Lock()
	return func() {
		flight.mu.Unlock()
		ls.flightMu.Lock()
		flight.refs--
		if flight.refs == 0 {
			delete(ls.flights, objectKey)
		}
		ls.flightMu.Unlock()
	}
}

func (ls *LocalStorage) SetMaxBytes(maxBytes int64) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.maxBytes = maxBytes
	if err := ls.evictLocked(0); err != nil {
		// 下次下载仍会再次尝试清理；这里不能让配置热重载失败。
		return
	}
}

// Prepare 为一个已知大小的文件腾出空间并创建临时缓存文件。未知大小或大于上限的资产仅代理、不缓存。
func (ls *LocalStorage) Prepare(objectKey string, size int64) (*os.File, string, error) {
	if objectKey == "" || size < 0 {
		return nil, "", ErrCacheDisabled
	}

	ls.mu.Lock()
	defer ls.mu.Unlock()

	if size > ls.maxBytes {
		return nil, "", ErrCacheDisabled
	}
	if err := ls.evictLocked(size); err != nil {
		return nil, "", err
	}

	dest := filepath.Join(ls.BaseDir, objectKey)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, "", err
	}

	temp := dest + ".cache.tmp"
	_ = os.Remove(temp)
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, "", err
	}
	return file, temp, nil
}

// Commit 将已写完且同步的临时文件原子发布为缓存文件。
func (ls *LocalStorage) Commit(tempPath, objectKey string) error {
	if err := os.Rename(tempPath, filepath.Join(ls.BaseDir, objectKey)); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

// RemoveRepo 删除仓库旧版本的全部缓存。
func (ls *LocalStorage) RemoveRepo(repoPath string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return os.RemoveAll(filepath.Join(ls.BaseDir, repoPath))
}

type cacheFile struct {
	path    string
	size    int64
	modTime int64
}

func (ls *LocalStorage) evictLocked(required int64) error {
	var (
		used  int64
		files []cacheFile
	)

	err := filepath.WalkDir(ls.BaseDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) == ".tmp" || filepath.Base(path) == "state.json" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		used += info.Size()
		files = append(files, cacheFile{path: path, size: info.Size(), modTime: info.ModTime().UnixNano()})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if used+required <= ls.maxBytes {
		return nil
	}

	sort.Slice(files, func(i, j int) bool { return files[i].modTime < files[j].modTime })
	for _, file := range files {
		if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		used -= file.size
		if used+required <= ls.maxBytes {
			return nil
		}
	}
	return ErrCacheDisabled
}
