package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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

	levelVar := new(slog.LevelVar)
	lvlStr := strings.ToLower(cfg.LogLevel)
	switch lvlStr {
	case "debug":
		levelVar.Set(slog.LevelDebug)
	case "info":
		levelVar.Set(slog.LevelInfo)
	case "warn", "warning":
		levelVar.Set(slog.LevelWarn)
	case "error", "err":
		levelVar.Set(slog.LevelError)
	default:
		levelVar.Set(slog.LevelWarn)
	}
	var writer io.Writer = os.Stderr
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			panic(err)
		}
		writer = io.MultiWriter(os.Stderr, f)
	}
	h := tint.NewHandler(writer, &tint.Options{AddSource: true, Level: levelVar, TimeFormat: "2006-01-02 15:04:05"})
	logger := slog.New(h)
	slog.SetDefault(logger)

	if cfg.Debug || lvlStr == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   128,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	client := &http.Client{Transport: transport}
	if cfg.RequestTimeout > 0 {
		client.Timeout = time.Duration(cfg.RequestTimeout) * time.Second
	}

	p := proxy.New(client, logger)

	r := gin.New()
	r.Use(middleware.Recovery(logger), middleware.RequestID(), middleware.Logger(logger))

	r.GET("/", proxy.HelloPage)
	r.GET("/metrics", middleware.MetricsHandler)
	r.Any("/proxy/*proxyPath", p.HandleProxyPath)
	r.Any(":protocol/*remainder", p.HandleProtocol)

	logger.Info("服务器启动", "addr", cfg.Addr(), "debug", cfg.Debug, "log_level", lvlStr, "version", version.Version, "commit", version.GitCommit)

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
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
