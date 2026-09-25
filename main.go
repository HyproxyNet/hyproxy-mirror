package main

import (
	"context"
	"flag"
	"html/template"
	"net/http"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"

	"hyproxy-mirror/internal/api"
	"hyproxy-mirror/internal/config"
	"hyproxy-mirror/internal/logx"
	"hyproxy-mirror/internal/storage"
	"hyproxy-mirror/internal/syncer"
)

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

	if err := config.Load(*cfgPath); err != nil {
		logx.Errorf("加载配置失败: %v", err)
		os.Exit(1)
	}
	logx.SetLevelFromString(config.GetLogLevel())
	logx.Infof("启动，日志等级: %s", config.GetLogLevel())

	baseDir := config.Cfg.Global.BaseDir
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		logx.Errorf("创建基目录失败: %v", err)
		os.Exit(1)
	}
	localStore := storage.NewLocal(baseDir, config.GetCacheMaxBytes())

	if err := syncer.LoadState("state.json"); err != nil {
		logx.Warnf("加载状态文件失败，将在首次同步后创建: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go syncer.RunScheduler(ctx, localStore)

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
			if err := config.Load(*cfgPath); err != nil {
				logx.Warnf("重新加载配置失败: %v", err)
				return
			}
			logx.SetLevelFromString(config.GetLogLevel())
			localStore.SetMaxBytes(config.GetCacheMaxBytes())
			logx.Infof("配置已重载，新的周期: %s，日志等级: %s，缓存上限: %s", config.GetInterval(), config.GetLogLevel(), config.Cfg.Global.CacheMaxSize)

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

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.Handle("/files/", http.StripPrefix("/files/", syncer.ServeAsset(localStore)))
	http.HandleFunc("/api/repos", api.Handler)
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
