package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lmittmann/tint"

	"anyproxy/internal/config"
	"anyproxy/internal/middleware"
	"anyproxy/internal/proxy"
	"anyproxy/internal/version"
)

func main() {
	cfg := config.Parse()

	// 日志初始化设置
	levelVar := new(slog.LevelVar)
	if cfg.Debug { levelVar.Set(slog.LevelDebug) } else { levelVar.Set(slog.LevelInfo) }
	var writer io.Writer = os.Stderr
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil { panic(err) }
		writer = io.MultiWriter(os.Stderr, f)
	}
	h := tint.NewHandler(writer, &tint.Options{AddSource: true, Level: levelVar, TimeFormat: "2006-01-02 15:04:05"})
	logger := slog.New(h)
	slog.SetDefault(logger)

	if cfg.Debug { gin.SetMode(gin.DebugMode) } else { gin.SetMode(gin.ReleaseMode) }

	// 可复用的 HTTP 客户端（保持连接复用）
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DisableCompression: true}
	client := &http.Client{Transport: transport}
	if cfg.RequestTimeout > 0 { client.Timeout = time.Duration(cfg.RequestTimeout) * time.Second }

	p := proxy.New(client, logger)

	r := gin.New()
	r.Use(middleware.Recovery(logger), middleware.RequestID(), middleware.Logger(logger))

	r.GET("/", proxy.HelloPage) // 欢迎页面
	r.Any("/proxy/*proxyPath", p.HandleProxyPath) // 处理 /proxy/*path 形式的请求
	r.Any(":protocol/*remainder", p.HandleProtocol) // 处理 /:protocol/*remainder 形式的请求

	logger.Info("服务器启动", "addr", cfg.Addr(), "debug", cfg.Debug, "version", version.Version, "commit", version.GitCommit)

	// 优雅停机设置：监听系统信号，执行平滑关闭
	srv := &http.Server{Addr: cfg.Addr(), Handler: r}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("服务器监听错误", "error", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("开始关闭 (收到退出信号)")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownGrace)*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("关闭出错", "error", err)
	} else {
		logger.Info("关闭完成")
	}
}
