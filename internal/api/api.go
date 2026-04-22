package api

import (
	"encoding/json"
	"net/http"

	"hyproxy-mirror/internal/logx"
	"hyproxy-mirror/internal/syncer"
)

// Handler 返回当前同步状态的 JSON
func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	status := syncer.GetStatus()
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	if err := enc.Encode(status); err != nil {
		logx.Errorf("API /api/repos 输出失败: %v", err)
		http.Error(w, "failed to encode status", http.StatusInternalServerError)
		return
	}

	logx.Debugf("API /api/repos 条目: %d", len(status))
}
