package router

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/logger"
)

// FrontendFallbackFS serves the SPA when no ./web directory exists on disk.
// The desktop build (cmd/desktop) points it at the frontend embedded in the
// executable (frontend/embed.go), so a single-binary install needs no
// sidecar web/ directory — otherwise "/" falls through to the auth
// middleware and the window renders a bare 401 JSON body.
var FrontendFallbackFS fs.FS

// serveFrontendStatic registers a middleware that serves the frontend SPA
// from the ./web directory if it exists, or from FrontendFallbackFS
// otherwise. Must be called BEFORE auth middleware so static files are
// served without authentication.
func serveFrontendStatic(r *gin.Engine) {
	webDir := os.Getenv("WEKNORA_WEB_DIR")
	if webDir == "" {
		webDir = "./web"
	}
	absDir, _ := filepath.Abs(webDir)
	indexPath := filepath.Join(absDir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		logger.Infof(context.Background(), "[Router] Serving frontend static files from %s", absDir)
		registerFrontendFS(r, os.DirFS(absDir))
		return
	}

	if FrontendFallbackFS != nil {
		if _, err := fs.Stat(FrontendFallbackFS, "index.html"); err == nil {
			logger.Infof(context.Background(), "[Router] Serving embedded frontend assets (no web/ directory on disk)")
			registerFrontendFS(r, FrontendFallbackFS)
			return
		}
		logger.Errorf(context.Background(),
			"[Router] No frontend to serve: %s has no index.html and no embedded bundle was captured. "+
				"Every non-API request will fall through to the auth middleware and return 401.", indexPath)
	} else {
		logger.Errorf(context.Background(),
			"[Router] No frontend to serve: %s is missing and no embedded bundle is configured (FrontendFallbackFS is nil). "+
				"Every non-API request will fall through to the auth middleware and return 401.", indexPath)
	}
}

// registerFrontendFS serves GET/HEAD of the SPA from fsys: existing files win,
// every other path falls back to index.html (history-mode routing).
func registerFrontendFS(r *gin.Engine, fsys fs.FS) {
	fileServer := http.FileServer(http.FS(fsys))
	r.Use(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/health") || strings.HasPrefix(path, "/swagger/") ||
			strings.HasPrefix(path, "/r/") || path == "/files" {
			c.Next()
			return
		}
		// Embed pages need the dedicated entry point and the channel CSP set by
		// embedFrameAncestorsMiddleware. Keep the main SPA same-origin only.
		if strings.HasPrefix(path, "/embed/") {
			if _, err := fs.Stat(fsys, "embed.html"); err == nil {
				http.ServeFileFS(c.Writer, c.Request, fsys, "embed.html")
				c.Abort()
				return
			}
			c.Next()
			return
		}
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Content-Security-Policy", "frame-ancestors 'self'")

		name := strings.TrimPrefix(path, "/")
		if name == "" {
			name = "index.html"
		}
		if info, err := fs.Stat(fsys, name); err == nil && !info.IsDir() {
			setFrontendCacheHeaders(c.Writer, path)
			fileServer.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		setFrontendCacheHeaders(c.Writer, "/index.html")
		http.ServeFileFS(c.Writer, c.Request, fsys, "index.html")
		c.Abort()
	})
}

// setFrontendCacheHeaders sets Cache-Control for frontend resources.
// Vite 构建产物中 /assets/* 的文件名带 hash，可长期缓存；其余（index.html、config.js、favicon 等）
// 每次都需 revalidate，避免前端升级后用户看到旧版本。
func setFrontendCacheHeaders(w http.ResponseWriter, path string) {
	if strings.HasPrefix(path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
}
