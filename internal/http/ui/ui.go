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

	serveIndex := func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	}
	serveHead := func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusOK)
	}

	engine.StaticFS("/sar-admin/assets", http.FS(assetFS))
	engine.GET("/sar-admin", serveIndex)
	engine.GET("/sar-admin/", serveIndex)
	engine.HEAD("/sar-admin", serveHead)
	engine.HEAD("/sar-admin/", serveHead)
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
