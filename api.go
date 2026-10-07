package main

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// Static React assets are shipped inside the binary; installed VMs need no Node.
//
//go:embed frontend/dist
var frontendAssets embed.FS

func newRouter() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery(), func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'")
		c.Next()
	})
	router.GET("/api/page", func(c *gin.Context) {
		tab := c.Query("tab")
		if tab != "" && tab != "status" && tab != "subscriptions" && tab != "devices" && tab != "routing" && tab != "gateway" && tab != "diagnostics" && tab != "backup" {
			c.JSON(400, gin.H{"error": "Неизвестная страница"})
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(200, pageData(c.Request))
	})
	router.GET("/api/dashboard", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(200, hostDashboard())
	})
	router.GET("/api/dashboard/history", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(200, readDashboardHistory())
	})
	router.GET("/api/dashboard/config", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		raw, err := dashboardConfig()
		if err != nil {
			c.JSON(404, gin.H{"error": "Применённая конфигурация ещё не создана или недоступна"})
			return
		}
		c.Data(200, "application/json", raw)
	})
	router.GET("/api/dashboard/logs", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		name := "xray-log"
		if c.Query("service") == "panel" {
			name = "panel-log"
		}
		raw, err := os.ReadFile(filepath.Join(stateDir(), name))
		if err != nil {
			c.JSON(200, gin.H{"text": "", "updated": ""})
			return
		}
		info, _ := os.Stat(filepath.Join(stateDir(), name))
		c.JSON(200, gin.H{"text": string(raw), "updated": info.ModTime().UTC().Format("2006-01-02T15:04:05Z")})
	})
	router.POST("/api/action", func(c *gin.Context) {
		if os.Getenv("NG_REVIEW") == "1" {
			action := c.PostForm("action")
			if operationKind(action) != "" || strings.Contains("|group-select|node-probe|source-probe|group-check|device-discover|dns-diagnose|xray-update-check|", "|"+action+"|") {
				c.JSON(403, gin.H{"ok": false, "message": "Проверка интерфейса: действия с рабочими службами отключены"})
				return
			}
		}
		c.Request.URL.Path = "/action"
		c.Request.Header.Set("Accept", "application/json")
		handler(c.Writer, c.Request)
	})
	assets, _ := fs.Sub(frontendAssets, "frontend/dist")
	router.GET("/LICENSE", func(c *gin.Context) {
		content, _ := fs.ReadFile(assets, "LICENSE")
		c.Data(200, "text/plain; charset=utf-8", content)
	})
	router.GET("/THIRD_PARTY_NOTICES.txt", func(c *gin.Context) {
		content, _ := fs.ReadFile(assets, "THIRD_PARTY_NOTICES.txt")
		c.Data(200, "text/plain; charset=utf-8", content)
	})
	router.GET("/assets/*path", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		http.FileServer(http.FS(assets)).ServeHTTP(c.Writer, c.Request)
	})
	router.GET("/", func(c *gin.Context) {
		if c.Query("legacy") == "1" {
			handler(c.Writer, c.Request)
			return
		}
		c.Header("Cache-Control", "no-store")
		content, _ := fs.ReadFile(assets, "index.html")
		c.Data(200, "text/html; charset=utf-8", content)
	})
	router.NoRoute(func(c *gin.Context) { c.Status(http.StatusOK); handler(c.Writer, c.Request) })
	return router
}
