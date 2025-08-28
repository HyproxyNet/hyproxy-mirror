package logx

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"
)

// Level 表示日志级别，数值越小级别越高
type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var current Level = LevelInfo

// SetLevel 设置日志级别
func SetLevel(l Level) { atomic.StoreInt32((*int32)(&current), int32(l)) }

// SetLevelFromString 通过字符串设置日志级别
func SetLevelFromString(s string) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		SetLevel(LevelDebug)
	case "info":
		SetLevel(LevelInfo)
	case "warn":
		SetLevel(LevelWarn)
	case "error":
		SetLevel(LevelError)
	default:
		SetLevel(LevelInfo)
	}
}

// shouldLog 判断是否应当输出该 level 的日志
func shouldLog(l Level) bool { return l >= Level(atomic.LoadInt32((*int32)(&current))) }

// prefix 生成统一前缀
func prefix(lv string) string {
	return fmt.Sprintf("%s [%s] ", time.Now().Format("2006-01-02 15:04:05"), lv)
}

// Debugf / Infof / Warnf / Errorf 为分级输出接口
func Debugf(format string, args ...any) {
	if shouldLog(LevelDebug) {
		log.Printf(prefix("DEBUG")+format, args...)
	}
}

func Infof(format string, args ...any) {
	if shouldLog(LevelInfo) {
		log.Printf(prefix("INFO ")+format, args...)
	}
}

func Warnf(format string, args ...any) {
	if shouldLog(LevelWarn) {
		log.Printf(prefix("WARN ")+format, args...)
	}
}

func Errorf(format string, args ...any) {
	if shouldLog(LevelError) {
		log.Printf(prefix("ERROR")+format, args...)
	}
}
