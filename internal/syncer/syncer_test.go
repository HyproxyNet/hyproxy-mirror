package syncer

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"hyproxy-mirror/internal/config"
	"hyproxy-mirror/internal/storage"
)

func TestServeAssetStreamsThenCaches(t *testing.T) {
	payload := []byte("downloaded through the mirror")
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "29")
		_, _ = w.Write(payload)
	}))
	defer upstream.Close()

	oldClient := httpClient
	httpClient = upstream.Client()
	defer func() { httpClient = oldClient }()

	oldConfig := config.Cfg
	defer func() { config.Cfg = oldConfig }()
	config.Cfg.Repositories = []config.RepoConfig{{Owner: "owner", Repo: "repo", Path: "repo"}}

	stateMu.Lock()
	oldState := stateData
	stateData = map[string]RepoState{
		"owner/repo": {Tag: "v1", AssetStatus: map[string]AssetStatus{"client.zip": {SourceURL: upstream.URL + "/client.zip"}}},
	}
	stateMu.Unlock()
	defer func() {
		stateMu.Lock()
		stateData = oldState
		stateMu.Unlock()
	}()

	store := storage.NewLocal(t.TempDir(), 1<<20)
	handler := ServeAsset(store)
	for i := 0; i < 2; i++ {
		request := httptest.NewRequest(http.MethodGet, "/repo/client.zip", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		result := response.Result()
		body, _ := io.ReadAll(result.Body)
		result.Body.Close()
		if result.StatusCode != http.StatusOK {
			t.Fatalf("request %d status = %d", i, result.StatusCode)
		}
		if string(body) != string(payload) {
			t.Fatalf("request %d body = %q, want %q", i, body, payload)
		}
	}

	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream request count = %d, want 1", got)
	}
}
