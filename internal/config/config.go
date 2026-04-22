package config

import (
	"errors"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

// GlobalConfig：全局配置；interval 用于调度周期；log_level 控制日志分级
type GlobalConfig struct {
	BaseDir          string `yaml:"base_dir"`
	Interval         string `yaml:"interval"`
	LogLevel         string `yaml:"log_level"`
	intervalDuration time.Duration
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
