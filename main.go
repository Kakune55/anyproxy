package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// 全局请求计数器，使用原子操作确保线程安全
var requestCounter int64



func main() {

	port := flag.Int("port", 8080, "代理服务器监听的端口")
	debug := flag.Bool("debug", false, "是否启用调试模式")
	logFile := flag.String("log", "", "日志文件路径，默认为标准输出")
	flag.Parse()

	// 配置slog
	var logger *slog.Logger
	
	// 根据调试模式设置日志级别
	var logLevel slog.Level
	if *debug {
		logLevel = slog.LevelDebug
	} else {
		logLevel = slog.LevelInfo
	}
	
	// 创建处理器选项
	opts := &slog.HandlerOptions{
		Level: logLevel,
	}
	
	if *logFile != "" {
		file, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			slog.Error("无法打开日志文件", "error", err)
			os.Exit(1)
		}
		logger = slog.New(slog.NewJSONHandler(file, opts))
	} else {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	slog.SetDefault(logger)


	if *debug {
		gin.SetMode(gin.DebugMode) // 启用调试模式
	} else {
		gin.SetMode(gin.ReleaseMode) // 在调试时暂时注释掉
	}
	
	r := gin.Default()

	// 处理根路径
	r.GET("/", HelloPage)
	
	// 使用 "catch-all" 路由来捕获所有代理请求
	// 这里我们使用 /proxy/* 前缀来避免与根路径冲突
	r.Any("/proxy/*proxyPath", proxyHandler)
	
	// 为了保持向后兼容，我们也可以处理直接的URL请求
	// 检查是否以协议开头的路径
	r.Any("/:protocol/*remainder", protocolHandler)

	slog.Info("HTTP 代理服务器启动", "port", *port)
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
	
	// 创建到目标服务器的请求
	// 注意：我们直接将原始请求的 Body 传递过去
	proxyReq, err := http.NewRequest(c.Request.Method, targetURLStr, c.Request.Body)
	if err != nil {
		slog.Error("创建代理请求失败", "reqID", reqID, "error", err)
		c.String(http.StatusInternalServerError, "创建代理请求失败: %v", err)
		return
	}

	// 复制原始请求的 Headers
	proxyReq.Header = c.Request.Header

	// 发送代理请求
	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		slog.Error("请求目标服务器失败", "reqID", reqID, "error", err)
		c.String(http.StatusBadGateway, "请求目标服务器失败: %s", err.Error())
		return
	}
	defer resp.Body.Close()

	slog.Debug("收到响应",
		"reqID", reqID,
		"status_code", resp.StatusCode,
		"status", resp.Status)
		
	// 复制目标服务器响应的 Headers 到原始响应
	for key, values := range resp.Header {
		for _, value := range values {
			c.Header(key, value)
		}
	}

	// 将目标服务器的响应状态码设置到原始响应
	c.Status(resp.StatusCode)

	// 将目标服务器的响应 Body 直接流式传输到客户端
	// 使用 io.Copy 更高效，并能处理各种编码（如 chunked）
	bytesCopied, err := io.Copy(c.Writer, resp.Body)
	if err != nil {
		slog.Error("写入响应 Body 时出错", "reqID", reqID, "error", err)
	}
	slog.Debug("响应写入完成", "reqID", reqID, "bytes_copied", bytesCopied)
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