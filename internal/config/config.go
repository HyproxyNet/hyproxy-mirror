package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

// GlobalConfig：全局配置；interval 用于调度周期；log_level 控制日志分级
type GlobalConfig struct {
	BaseDir          string `yaml:"base_dir"`
	Interval         string `yaml:"interval"`
	LogLevel         string `yaml:"log_level"`
	CacheMaxSize     string `yaml:"cache_max_size"`
	intervalDuration time.Duration
	cacheMaxBytes    int64
}

// RepoConfig：单个仓库配置
type RepoConfig struct {
	Owner string `yaml:"owner"`
	Repo  string `yaml:"repo"`
	Path  string `yaml:"path"`
}

// Config：总配置
type Config struct {
	Global       GlobalConfig `yaml:"global"`
	Repositories []RepoConfig `yaml:"repositories"`
}

var Cfg Config

const defaultCacheMaxSize = "20GB"

func parseByteSize(value string) (int64, error) {
	value = strings.TrimSpace(strings.ToUpper(value))
	for _, suffix := range []struct {
		name       string
		multiplier int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	} {
		if strings.HasSuffix(value, suffix.name) {
			number := strings.TrimSpace(strings.TrimSuffix(value, suffix.name))
			amount, err := strconv.ParseInt(number, 10, 64)
			if err != nil || amount <= 0 || amount > (1<<63-1)/suffix.multiplier {
				return 0, fmt.Errorf("invalid size %q", value)
			}
			return amount * suffix.multiplier, nil
		}
	}
	return 0, fmt.Errorf("invalid size %q (use B, KB, MB, GB, or TB)", value)
}

// Load 读取并解析 YAML，填充默认值与基本校验
func Load(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, &Cfg); err != nil {
		return err
	}

	if Cfg.Global.BaseDir == "" {
		Cfg.Global.BaseDir = "data"
	}
	if Cfg.Global.Interval == "" {
		Cfg.Global.Interval = "1h"
	}
	if Cfg.Global.LogLevel == "" {
		Cfg.Global.LogLevel = "info"
	}
	if Cfg.Global.CacheMaxSize == "" {
		Cfg.Global.CacheMaxSize = defaultCacheMaxSize
	}

	cacheMaxBytes, err := parseByteSize(Cfg.Global.CacheMaxSize)
	if err != nil {
		return fmt.Errorf("invalid global.cache_max_size: %w", err)
	}
	Cfg.Global.cacheMaxBytes = cacheMaxBytes

	d, err := time.ParseDuration(Cfg.Global.Interval)
	if err != nil || d <= 0 {
		d = time.Hour
	}
	Cfg.Global.intervalDuration = d

	switch strings.ToLower(Cfg.Global.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		Cfg.Global.LogLevel = "info"
	}

	if len(Cfg.Repositories) == 0 {
		return errors.New("no repositories configured")
	}
	return nil
}

// GetInterval 返回解析后的调度周期
func GetInterval() time.Duration {
	return Cfg.Global.intervalDuration
}

// GetLogLevel 返回标准化日志级别字符串
func GetLogLevel() string {
	return strings.ToLower(Cfg.Global.LogLevel)
}

// GetCacheMaxBytes 返回本地缓存允许占用的最大空间。
func GetCacheMaxBytes() int64 {
	return Cfg.Global.cacheMaxBytes
}
