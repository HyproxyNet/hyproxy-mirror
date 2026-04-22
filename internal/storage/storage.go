package storage

import (
	"io"
	"os"
	"path/filepath"
)

// Storage 定义统一的持久化接口
type Storage interface {
	Save(localFilePath, objectKey string) error
}

// LocalStorage 将文件保存到本地目录
type LocalStorage struct {
	BaseDir string
}

func NewLocal(baseDir string) Storage {
	return &LocalStorage{BaseDir: baseDir}
}

// Save 将已下载完成的临时文件移动/复制到目标位置；跨分区时回退为复制并 fsync
func (ls *LocalStorage) Save(localFilePath, objectKey string) error {
	if objectKey == "" {
		return os.ErrInvalid
	}

	dest := filepath.Join(ls.BaseDir, objectKey)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	// 优先尝试原子重命名
	if err := os.Rename(localFilePath, dest); err == nil {
		return nil
	}

	// 回退：安全复制
	src, err := os.Open(localFilePath)
	if err != nil {
		return err
	}
	defer src.Close()

	tmp := dest + ".cp.tmp"
	_ = os.Remove(tmp)

	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	// 原子替换
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	_ = os.Remove(localFilePath)
	return nil
}
