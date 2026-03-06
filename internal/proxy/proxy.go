package proxy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"anyproxy/internal/metrics"
	"anyproxy/internal/middleware"
)

// 转发的总请求计数器
var totalForwarded atomic.Int64

var copyBufPool = sync.Pool{
	New: func() any { return make([]byte, 32*1024) },
}

// Proxy 封装具体的转发逻辑
type Proxy struct {
	Client *http.Client
	Log    *slog.Logger
}

func New(client *http.Client, logger *slog.Logger) *Proxy {
	return &Proxy{Client: client, Log: logger}
}

// HandleProxyPath 处理 /proxy/*path 形式的请求
func (p *Proxy) HandleProxyPath(c *gin.Context) {
	urlStr, err := BuildFromProxyPath(c.Param("proxyPath"), c.Request.URL.Query())
	if err != nil {
		p.writeError(c, http.StatusBadRequest, err)
		return
	}
	p.forward(c, urlStr)
}

// HandleProtocol 处理 /:protocol/*remainder 形式的请求
func (p *Proxy) HandleProtocol(c *gin.Context) {
	urlStr, err := BuildFromProtocol(c.Param("protocol"), c.Param("remainder"), c.Request.URL.Query())
	if err != nil {
		p.writeError(c, http.StatusBadRequest, err)
		return
	}
	p.forward(c, urlStr)
}

func (p *Proxy) writeError(c *gin.Context, code int, err error) {
	c.JSON(code, gin.H{"error": err.Error(), "req_id": middleware.GetReqID(c)})
}

func (p *Proxy) forward(c *gin.Context, target string) {
	reqID := middleware.GetReqID(c)
	current := totalForwarded.Add(1)
	p.Log.Debug("开始转发请求",
		"req_id", reqID,
		"count", current,
		"method", c.Request.Method,
		"target", target,
		"uri", c.Request.RequestURI,
	)

	// 基于原始上下文创建上游请求（支持客户端断开时取消）
	upReq, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, c.Request.Body)
	if err != nil {
		p.Log.Error("创建上游请求失败", "req_id", reqID, "error", err)
		p.writeError(c, http.StatusInternalServerError, errors.New("创建上游请求失败"))
		return
	}
	upReq.Header = c.Request.Header.Clone()
	if strings.Contains(strings.ToLower(c.GetHeader("Accept")), "text/event-stream") {
		// SSE 禁用压缩
		upReq.Header.Del("Accept-Encoding")
	}

	resp, err := p.Client.Do(upReq)
	if err != nil {
		p.Log.Error("上游请求失败", "req_id", reqID, "error", err)
		p.writeError(c, http.StatusBadGateway, errors.New("上游请求失败"))
		return
	}
	defer resp.Body.Close()

	// 仅在真正进行了一次上游转发并得到响应后计数
	metrics.Inc()

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	isSSE := strings.HasPrefix(contentType, "text/event-stream")

	p.Log.Debug("上游响应", "req_id", reqID, "status", resp.StatusCode, "sse", isSSE)

	// 复制上游响应头
	dstHeader := c.Writer.Header()
	for k, vs := range resp.Header { dstHeader[k] = vs }
	if isSSE {
		c.Writer.Header().Del("Content-Length")
		c.Writer.Header().Del("Transfer-Encoding")
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
	}
	c.Status(resp.StatusCode)
	if f, ok := c.Writer.(http.Flusher); ok { f.Flush() }

	if !isSSE {
		buf := copyBufPool.Get().([]byte)
		_, err := io.CopyBuffer(c.Writer, resp.Body, buf)
		copyBufPool.Put(buf)
		if err != nil {
			p.Log.Error("写入响应体失败", "req_id", reqID, "error", err)
		}
		return
	}

	reader := bufio.NewReader(resp.Body)
	w := c.Writer
	flusher, _ := w.(http.Flusher)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, werr := w.Write(line); werr != nil {
				p.Log.Warn("SSE写入失败", "req_id", reqID, "error", werr)
				return
			}
			if flusher != nil { flusher.Flush() }
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				p.Log.Debug("SSE结束(EOF)", "req_id", reqID)
			} else {
				p.Log.Error("SSE读取失败", "req_id", reqID, "error", err)
			}
			return
		}
	}
}

// HelloPage 返回简单状态页面
func HelloPage(c *gin.Context) {
	count := metrics.Total()
	qps := metrics.QPS()
	qpm := metrics.QPM()

	// 推断外部可见协议与主机（支持反向代理常见头）
	scheme := "http"
	if c.Request.TLS != nil { scheme = "https" }
	if xf := c.GetHeader("X-Forwarded-Proto"); xf != "" {
		// 取第一个
		scheme = strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	host := c.Request.Host
	if xfh := c.GetHeader("X-Forwarded-Host"); xfh != "" {
		host = strings.TrimSpace(strings.Split(xfh, ",")[0])
	}
	base := scheme + "://" + host

	str := fmt.Sprintf("AnyProxy 服务器正在运行...\n累计转发(不含本页): %d\n当前QPS: %d\n最近1分钟QPM: %d", count, qps, qpm)
	str += "\n\n使用方法:\n"
	str += "方式1 - 直接协议路径: \n"
	str += fmt.Sprintf("  目标URL: https://example.com/path --> 代理URL: %s/https/example.com/path\n", base)
	str += fmt.Sprintf("  目标URL: http://example.com/path  --> 代理URL: %s/http/example.com/path\n\n", base)
	str += "方式2 - 完整URL路径: \n"
	str += fmt.Sprintf("  目标URL: https://example.com --> 代理URL: %s/proxy/https://example.com\n", base)
	str += fmt.Sprintf("  目标URL: http://example.com  --> 代理URL: %s/proxy/http://example.com\n\n", base)
	str += "目标URL必须以 https:// 或 http:// 开头。\n\n"
	str += fmt.Sprintf("本机访问基地址: %s\n", base)
	c.String(200, str)
}
