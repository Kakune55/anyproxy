package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lmittmann/tint"
)

// 全局请求计数器，使用原子操作确保线程安全
var requestCounter int64



func main() {
    port := flag.Int("port", 8080, "代理服务器监听的端口")
    debug := flag.Bool("debug", false, "是否启用调试模式")
    logFile := flag.String("log", "", "日志文件路径，默认为控制台彩色输出")
    flag.Parse()

    // 使用 tint + LevelVar
    var levelVar = new(slog.LevelVar)
    if *debug {
        levelVar.Set(slog.LevelDebug)
    } else {
        levelVar.Set(slog.LevelInfo)
    }

    // 组合输出 writer
    var writer io.Writer = os.Stderr // 默认彩色输出到 stderr
    if *logFile != "" {
        f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
        if err != nil {
            fmt.Fprintf(os.Stderr, "无法打开日志文件: %v\n", err)
            os.Exit(1)
        }
        // 同时输出到彩色终端和文件（文件里不需要颜色，tint 会根据是否是终端决定）
        writer = io.MultiWriter(os.Stderr, f)
    }

    handler := tint.NewHandler(writer, &tint.Options{
        AddSource:  true,
        Level:      levelVar,
        TimeFormat: "2006-01-02 15:04:05",
    })
    slog.SetDefault(slog.New(handler))

    if *debug {
        gin.SetMode(gin.DebugMode)
    } else {
        gin.SetMode(gin.ReleaseMode)
    }

	r := gin.New() // 不使用默认 Logger，改为自定义 slog 统一输出
	r.Use(SlogLogger(), SlogRecovery())
	r.GET("/", HelloPage)
    r.Any("/proxy/*proxyPath", proxyHandler)
    r.Any(":protocol/*remainder", protocolHandler)

    slog.Info("HTTP 代理服务器启动", "port", *port, "debug", *debug)
    if err := r.Run(fmt.Sprintf(":%d", *port)); err != nil {
        slog.Error("启动服务器失败", "error", err)
    }
}

// normalizeURL 规范化URL格式，处理缺少斜杠的情况
func normalizeURL(rawURL string) string {
	// 处理 https:/example.com 或 http:/example.com 的情况
	if strings.HasPrefix(rawURL, "https:/") && !strings.HasPrefix(rawURL, "https://") {
		return strings.Replace(rawURL, "https:/", "https://", 1)
	}
	if strings.HasPrefix(rawURL, "http:/") && !strings.HasPrefix(rawURL, "http://") {
		return strings.Replace(rawURL, "http:/", "http://", 1)
	}
	return rawURL
}

func proxyHandler(c *gin.Context) {
	// 从路径参数中获取目标 URL
	targetURLStr := c.Param("proxyPath")
	// 移除前导斜杠
	targetURLStr = strings.TrimPrefix(targetURLStr, "/")
	
	// 规范化URL格式
	targetURLStr = normalizeURL(targetURLStr)

	// 解析目标URL
	parsedURL, err := url.Parse(targetURLStr)
	if err != nil {
		c.String(http.StatusBadRequest, "无效的目标 URL: %v", err)
		return
	}

	// 合并查询参数
	originalQuery := c.Request.URL.Query()
	targetQuery := parsedURL.Query()
	for key, values := range originalQuery {
		for _, value := range values {
			targetQuery.Add(key, value)
		}
	}
	parsedURL.RawQuery = targetQuery.Encode()
	
	// 重新构建目标URL字符串
	targetURLStr = parsedURL.String()

	// 检查 URL 合法性
	if _, err := url.ParseRequestURI(targetURLStr); err != nil {
		c.String(http.StatusBadRequest, "无效的目标 URL: %v", err)
		return
	}

	// 执行代理请求
	executeProxy(c, targetURLStr)
}

// protocolHandler 处理直接以协议开头的URL请求 (如 /https/example.com/path)
func protocolHandler(c *gin.Context) {
	protocol := c.Param("protocol")
	remainder := c.Param("remainder")
	
	// 只处理 http 和 https 协议
	if protocol != "http" && protocol != "https" {
		c.String(http.StatusBadRequest, "不支持的协议: %s", protocol)
		return
	}
	
	// 构建完整的URL
	targetURLStr := protocol + ":/" + remainder
	
	// 规范化URL格式
	targetURLStr = normalizeURL(targetURLStr)

	// 解析目标URL
	parsedURL, err := url.Parse(targetURLStr)
	if err != nil {
		c.String(http.StatusBadRequest, "无效的目标 URL: %v", err)
		return
	}

	// 合并查询参数
	originalQuery := c.Request.URL.Query()
	targetQuery := parsedURL.Query()
	for key, values := range originalQuery {
		for _, value := range values {
			targetQuery.Add(key, value)
		}
	}
	parsedURL.RawQuery = targetQuery.Encode()

	// 重新构建目标URL字符串
	targetURLStr = parsedURL.String()

	// 检查 URL 合法性
	if _, err := url.ParseRequestURI(targetURLStr); err != nil {
		c.String(http.StatusBadRequest, "无效的目标 URL: %v", err)
		return
	}

	// 执行代理请求
	executeProxy(c, targetURLStr)
}

// executeProxy 执行实际的代理请求
func executeProxy(c *gin.Context, targetURLStr string) {
	// 增加请求计数器
	reqID := atomic.AddInt64(&requestCounter, 1)

	slog.Debug("收到请求",
		"reqID", reqID,
		"method", c.Request.Method,
		"uri", c.Request.RequestURI,
		"target", targetURLStr)

	// 自定义 Transport，禁止自动压缩（避免 gzip 聚合导致 SSE 延迟）
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DisableCompression:  true,
	}
	client := &http.Client{Transport: transport}

	// 创建到目标服务器的请求
	proxyReq, err := http.NewRequest(c.Request.Method, targetURLStr, c.Request.Body)
	if err != nil {
		slog.Error("创建代理请求失败", "reqID", reqID, "error", err)
		c.String(http.StatusInternalServerError, "创建代理请求失败: %v", err)
		return
	}

	// 复制原始请求的 Headers (Clone 避免引用共享)
	proxyReq.Header = c.Request.Header.Clone()
	// 禁止上游压缩，保证事件粒度
	proxyReq.Header.Del("Accept-Encoding")

	resp, err := client.Do(proxyReq)
	if err != nil {
		slog.Error("请求目标服务器失败", "reqID", reqID, "error", err)
		c.String(http.StatusBadGateway, "请求目标服务器失败: %s", err.Error())
		return
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	isSSE := strings.HasPrefix(contentType, "text/event-stream")

	slog.Debug("收到响应", "reqID", reqID, "status_code", resp.StatusCode, "status", resp.Status, "isSSE", isSSE)

	// 复制响应头
	for key, values := range resp.Header {
		for _, value := range values {
			c.Header(key, value)
		}
	}
	// SSE 需要去掉不合适的头并设置必要头
	if isSSE {
		c.Writer.Header().Del("Content-Length")
		c.Writer.Header().Del("Transfer-Encoding")
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no") // 防止某些反向代理缓冲
	}

	// 设置状态码
	c.Status(resp.StatusCode)

	// 立即 flush 头部，尤其是 SSE
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}

	if !isSSE {
		// 普通请求直接复制主体
		bytesCopied, err := io.Copy(c.Writer, resp.Body)
		if err != nil {
			slog.Error("写入响应 Body 时出错", "reqID", reqID, "error", err)
		}
		slog.Debug("响应写入完成", "reqID", reqID, "bytes_copied", bytesCopied)
		return
	}

	// SSE 模式：逐行读取并 flush，保持事件实时性
	reader := bufio.NewReader(resp.Body)
	w := c.Writer
	flusher, _ := w.(http.Flusher)

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, werr := w.Write(line); werr != nil {
				slog.Warn("SSE 写失败", "reqID", reqID, "error", werr)
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Debug("SSE 结束(EOF)", "reqID", reqID)
			} else {
				slog.Error("读取 SSE 失败", "reqID", reqID, "error", err)
			}
			return
		}
	}
}


func HelloPage(c *gin.Context) {
	// 获取当前的请求计数
	count := atomic.LoadInt64(&requestCounter)
	str := fmt.Sprintf("AnyProxy 服务器正在运行... 已转发 %d 个请求", count)
	str += "\n\n使用方法:\n"
	str += "方式1 - 直接协议路径: \n"
	str += "  目标URL: https://example.com/path --> 代理URL: http://AnyproxyIP/https/example.com/path\n"
	str += "  目标URL: http://example.com/path --> 代理URL: http://AnyproxyIP/http/example.com/path\n\n"
	str += "方式2 - 完整URL路径: \n"
	str += "  目标URL: https://example.com --> 代理URL: http://AnyproxyIP/proxy/https://example.com\n\n"
	str += "目标URL必须以 https:// 或 http:// 开头。\n\n"
	c.String(200, str)
}

// SlogLogger 统一请求日志中间件
func SlogLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		rawQuery := c.Request.URL.RawQuery
		c.Next()
		latency := time.Since(start)
		status := c.Writer.Status()
		size := c.Writer.Size()
		method := c.Request.Method
		ip := c.ClientIP()
		if rawQuery != "" {
			path = path + "?" + rawQuery
		}
		slog.Log(c, slog.LevelInfo, "HTTP 请求",
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("latency", latency),
			slog.Int("size", size),
			slog.String("ip", ip),
			slog.String("ua", c.GetHeader("User-Agent")),
		)
	}
}

// SlogRecovery 捕获 panic，输出堆栈
func SlogRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rcv := recover(); rcv != nil {
				stack := debug.Stack()
				slog.Error("发生 panic",
					"error", rcv,
					"stack", string(stack),
					"path", c.Request.URL.Path,
				)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}