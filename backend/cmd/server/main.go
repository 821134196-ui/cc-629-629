package main

import (
	"embed"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"mealsubsidy/internal/api"
	"mealsubsidy/internal/service"
	"mealsubsidy/internal/store"
)

//go:embed all:web
var webFS embed.FS

func main() {
	dsn := envOrDefault("DB_PATH", "data/mealsubsidy.db")
	_ = os.MkdirAll(filepath.Dir(dsn), 0o755)
	db := store.Open(dsn)
	if os.Getenv("NO_SEED") == "" {
		seedIfEmpty(db)
	}
	svc := service.New(db)

	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()
	api.New(svc).Register(r)

	registerFrontend(r)

	addr := envOrDefault("ADDR", ":8080")
	log.Printf("社区助餐系统已启动： http://localhost%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// registerFrontend 服务内嵌前端构建产物；非 /api 且无对应静态文件的路径回退到 index.html（SPA）。
func registerFrontend(r *gin.Engine) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.URL.Path == "/api" {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		p := strings.TrimPrefix(c.Request.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if serveEmbedded(c, sub, p) {
			return
		}
		// SPA 回退：任何未知路径都返回 index.html（200，不做 301）
		serveEmbedded(c, sub, "index.html")
	})
}

func serveEmbedded(c *gin.Context, sub fs.FS, name string) bool {
	f, err := sub.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		return false
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		return false
	}
	c.Status(http.StatusOK)
	http.ServeContent(c.Writer, c.Request, stat.Name(), stat.ModTime(), rs)
	return true
}
