package main

import (
	"context"
	"flag"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"hyproxy-mirror/internal/api"
	"hyproxy-mirror/internal/config"
	"hyproxy-mirror/internal/logx"
	"hyproxy-mirror/internal/storage"
	"hyproxy-mirror/internal/syncer"
)

// 仅初始化模板
var tpl *template.Template

func init() {
	t, err := template.ParseGlob("templates/*.html")
	if err != nil {
		panic(err)
	}
	tpl = t
}

func main() {
	cfgPath := flag.String("config", "config.yaml", "YAML 配置文件路径")
	addr := flag.String("addr", ":8080", "HTTP 监听地址")
	flag.Parse()

	// 首次加载配置并设置日志级别
	if err := config.Load(*cfgPath); err != nil {
		logx.Errorf("加载配置失败: %v", err)
		os.Exit(1)
	}
	logx.SetLevelFromString(config.GetLogLevel())
	logx.Infof("启动，日志等级: %s", config.GetLogLevel())

	// 基目录
	base := config.Cfg.Global.BaseDir
	if err := os.MkdirAll(base, 0o755); err != nil {
		logx.Errorf("创建基目录失败: %v", err)
		os.Exit(1)
	}

	localStore := storage.NewLocal(base)

	// 状态文件
	if err := syncer.LoadState("state.json"); err != nil {
		logx.Warnf("加载状态文件失败，将在首次同步后创建: %v", err)
	}

	// 调度上下文
	ctx, cancel := context.WithCancel(context.Background())
	go syncer.RunScheduler(ctx, localStore)

	// 配置热更新（去抖 500ms）
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logx.Errorf("创建文件监控失败: %v", err)
		os.Exit(1)
	}
	defer watcher.Close()

	if err := watcher.Add(*cfgPath); err != nil {
		logx.Errorf("监控配置文件失败: %v", err)
		os.Exit(1)
	}

	go func() {
		var (
			debounce *time.Timer
			pending  bool
		)

		restart := func() {
			// 先加载新配置
			if err := config.Load(*cfgPath); err != nil {
				logx.Warnf("重新加载配置失败: %v", err)
				return
			}
			// 更新日志等级
			logx.SetLevelFromString(config.GetLogLevel())
			logx.Infof("配置已重载，新的周期: %s，日志等级: %s", config.GetInterval(), config.GetLogLevel())

			// 重启调度
			cancel()
			ctx, cancel = context.WithCancel(context.Background())
			go syncer.RunScheduler(ctx, localStore)
		}

		for ev := range watcher.Events {
			if ev.Op&fsnotify.Write == fsnotify.Write {
				if !pending {
					pending = true
					debounce = time.AfterFunc(500*time.Millisecond, func() {
						pending = false
						restart()
					})
				} else if debounce != nil {
					debounce.Reset(500 * time.Millisecond)
				}
			}
		}
	}()

	// 静态文件与 API
	http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir(filepath.Clean(base)))))
	http.HandleFunc("/api/repos", api.Handler)

	// 首页模板渲染
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := syncer.GetStatus()
		if err := tpl.ExecuteTemplate(w, "index.html", data); err != nil {
			logx.Errorf("模板渲染失败: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	logx.Infof("服务启动: http://localhost%s", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		logx.Errorf("服务器异常: %v", err)
		os.Exit(1)
	}
}
