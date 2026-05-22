package ui

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed static/*
var staticFiles embed.FS

func Register(engine *gin.Engine) {
	assetFS := mustSub("static/assets")
	indexHTML := mustReadFile("static/index.html")

	uiGroup := engine.Group("/sar-admin")
	uiGroup.Use(noStore)

	serveIndex := func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	}
	serveHead := func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusOK)
	}

	uiGroup.StaticFS("/assets", http.FS(assetFS))
	uiGroup.GET("", serveIndex)
	uiGroup.GET("/", serveIndex)
	uiGroup.HEAD("", serveHead)
	uiGroup.HEAD("/", serveHead)
}

func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Next()
}

func mustSub(path string) fs.FS {
	subFS, err := fs.Sub(staticFiles, path)
	if err != nil {
		panic(err)
	}
	return subFS
}

func mustReadFile(path string) []byte {
	content, err := staticFiles.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return content
}
