package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"code-traefik/handlers"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadAgentBinary(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("SuccessDownloadWhenFileExists", func(t *testing.T) {
		tempDir := t.TempDir()
		dummyBinPath := filepath.Join(tempDir, "code-traefik-agent")
		dummyContent := []byte("#!/bin/sh\necho 'code-traefik-agent-binary-content'\n")
		err := os.WriteFile(dummyBinPath, dummyContent, 0755)
		require.NoError(t, err)

		t.Setenv("TRAEFIK_AGENT_BIN_PATH", dummyBinPath)

		r := gin.New()
		r.GET("/api/agent/download", handlers.DownloadAgentBinary)

		req, err := http.NewRequest(http.MethodGet, "/api/agent/download", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment; filename=\"code-traefik-agent\"")
		assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
		assert.Equal(t, dummyContent, w.Body.Bytes())
	})

	t.Run("NotFoundWhenFileDoesNotExist", func(t *testing.T) {
		tempDir := t.TempDir()
		nonExistentPath := filepath.Join(tempDir, "non-existent-agent-bin")
		t.Setenv("TRAEFIK_AGENT_BIN_PATH", nonExistentPath)

		r := gin.New()
		r.GET("/api/agent/download", handlers.DownloadAgentBinary)

		req, err := http.NewRequest(http.MethodGet, "/api/agent/download", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// 由于系统根目录可能存在编译好的 ./code-traefik-agent，如果环境变量设置的路径不存在，候选路径还会尝试 ./code-traefik-agent
		// 若要确保走到 404 分支，我们将 working directory 指向空临时目录测试
		origWd, _ := os.Getwd()
		_ = os.Chdir(tempDir)
		defer func() { _ = os.Chdir(origWd) }()

		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req)

		assert.Equal(t, http.StatusNotFound, w2.Code)
		assert.Contains(t, w2.Body.String(), "服务端未找到可供下载")
	})
}
