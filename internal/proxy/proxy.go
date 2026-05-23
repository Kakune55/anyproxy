package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"anyproxy/internal/metrics"
	"anyproxy/internal/middleware"
)

var totalForwarded atomic.Int64

var copyBufPool = sync.Pool{
	New: func() any { return make([]byte, 32*1024) },
}

const maxReplayBodyBytes int64 = 8 << 20

var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Proxy-Connection",
	"TE",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

type readCloser struct {
	io.Reader
	closer io.Closer
}

func (r *readCloser) Close() error {
	return r.closer.Close()
}

func removeHopByHopHeaders(h http.Header) {
	for _, header := range h.Values("Connection") {
		for _, f := range strings.Split(header, ",") {
			if f = strings.TrimSpace(f); f != "" {
				h.Del(f)
			}
		}
	}
	for _, header := range hopByHopHeaders {
		h.Del(header)
	}
}

type upstreamTrace struct {
	start             time.Time
	dnsStart          time.Time
	dnsDone           time.Time
	connectStart      time.Time
	connectDone       time.Time
	tlsStart          time.Time
	tlsDone           time.Time
	wroteRequest      time.Time
	firstResponseByte time.Time
	reusedConn        bool
}

func newUpstreamTrace() *upstreamTrace {
	return &upstreamTrace{start: time.Now()}
}

func (t *upstreamTrace) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			t.dnsStart = time.Now()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			t.dnsDone = time.Now()
		},
		ConnectStart: func(_, _ string) {
			t.connectStart = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			t.connectDone = time.Now()
		},
		TLSHandshakeStart: func() {
			t.tlsStart = time.Now()
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			t.tlsDone = time.Now()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			t.reusedConn = info.Reused
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			t.wroteRequest = time.Now()
		},
		GotFirstResponseByte: func() {
			t.firstResponseByte = time.Now()
		},
	}
}

func (t *upstreamTrace) attrs() []any {
	attrs := []any{
		"upstream_total_ms", time.Since(t.start).Milliseconds(),
		"upstream_reused_conn", t.reusedConn,
	}
	if !t.dnsStart.IsZero() && !t.dnsDone.IsZero() {
		attrs = append(attrs, "upstream_dns_ms", t.dnsDone.Sub(t.dnsStart).Milliseconds())
	}
	if !t.connectStart.IsZero() && !t.connectDone.IsZero() {
		attrs = append(attrs, "upstream_connect_ms", t.connectDone.Sub(t.connectStart).Milliseconds())
	}
	if !t.tlsStart.IsZero() && !t.tlsDone.IsZero() {
		attrs = append(attrs, "upstream_tls_ms", t.tlsDone.Sub(t.tlsStart).Milliseconds())
	}
	if !t.wroteRequest.IsZero() {
		attrs = append(attrs, "upstream_to_write_req_ms", t.wroteRequest.Sub(t.start).Milliseconds())
	}
	if !t.firstResponseByte.IsZero() {
		attrs = append(attrs, "upstream_to_first_byte_ms", t.firstResponseByte.Sub(t.start).Milliseconds())
	}
	return attrs
}

func (t *upstreamTrace) phase() string {
	switch {
	case !t.firstResponseByte.IsZero():
		return "reading_response"
	case !t.wroteRequest.IsZero():
		return "waiting_first_byte"
	case !t.tlsStart.IsZero() && t.tlsDone.IsZero():
		return "tls_handshake"
	case !t.connectStart.IsZero() && t.connectDone.IsZero():
		return "tcp_connect"
	case !t.dnsStart.IsZero() && t.dnsDone.IsZero():
		return "dns_lookup"
	default:
		return "before_request_sent"
	}
}

func classifyUpstreamError(ctx context.Context, err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return "client_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "deadline_exceeded"
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsTimeout {
			return "dns_timeout"
		}
		if dnsErr.IsNotFound {
			return "dns_not_found"
		}
		return "dns_error"
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "network_timeout"
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Op != "" {
			return opErr.Op + "_error"
		}
		return "network_error"
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Op != "" {
		if strings.Contains(urlErr.Err.Error(), "http2: Transport: cannot retry") {
			return "http2_retry_body_unavailable"
		}
		return urlErr.Op + "_error"
	}

	if strings.Contains(err.Error(), "http2: Transport: cannot retry") {
		return "http2_retry_body_unavailable"
	}

	return "upstream_error"
}

func prepareUpstreamBody(req *http.Request) (io.ReadCloser, func() (io.ReadCloser, error), bool, int64, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return http.NoBody, nil, false, 0, nil
	}
	if req.ContentLength > maxReplayBodyBytes {
		return req.Body, nil, false, req.ContentLength, nil
	}

	body, err := io.ReadAll(io.LimitReader(req.Body, maxReplayBodyBytes+1))
	if err != nil {
		return nil, nil, false, 0, err
	}

	if int64(len(body)) <= maxReplayBodyBytes {
		if err := req.Body.Close(); err != nil {
			return nil, nil, false, 0, err
		}
		getBody := func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
		return io.NopCloser(bytes.NewReader(body)), getBody, true, int64(len(body)), nil
	}

	return &readCloser{
		Reader: io.MultiReader(bytes.NewReader(body), req.Body),
		closer: req.Body,
	}, nil, false, req.ContentLength, nil
}

type Proxy struct {
	Client *http.Client
	Log    *slog.Logger
}

func New(client *http.Client, logger *slog.Logger) *Proxy {
	return &Proxy{Client: client, Log: logger}
}

func (p *Proxy) HandleProxyPath(c *gin.Context) {
	urlStr, err := BuildFromProxyPath(c.Param("proxyPath"), c.Request.URL.Query())
	if err != nil {
		p.writeError(c, http.StatusBadRequest, err)
		return
	}
	p.forward(c, urlStr)
}

func (p *Proxy) HandleProtocol(c *gin.Context) {
	urlStr, err := BuildFromProtocol(c.Param("protocol"), c.Param("remainder"), c.Request.URL.Query())
	if err != nil {
		p.writeError(c, http.StatusBadRequest, err)
		return
	}
	p.forward(c, urlStr)
}

func (p *Proxy) writeError(c *gin.Context, code int, err error) {
	c.JSON(code, gin.H{"error": err.Error(), "req_id": middleware.GetReqID(c), "source": "anyproxy"})
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

	trace := newUpstreamTrace()
	ctx := httptrace.WithClientTrace(c.Request.Context(), trace.clientTrace())

	body, getBody, bodyReplayable, contentLength, err := prepareUpstreamBody(c.Request)
	if err != nil {
		p.Log.Error("读取请求体失败", "req_id", reqID, "error", err)
		p.writeError(c, http.StatusBadRequest, errors.New("读取请求体失败"))
		return
	}

	upReq, err := http.NewRequestWithContext(ctx, c.Request.Method, target, body)
	if err != nil {
		p.Log.Error("创建上游请求失败", "req_id", reqID, "error", err)
		p.writeError(c, http.StatusInternalServerError, errors.New("创建上游请求失败"))
		return
	}
	upReq.GetBody = getBody
	if getBody != nil {
		upReq.ContentLength = contentLength
	}
	upReq.Header = c.Request.Header.Clone()
	removeHopByHopHeaders(upReq.Header)
	if strings.Contains(strings.ToLower(c.GetHeader("Accept")), "text/event-stream") {
		upReq.Header.Del("Accept-Encoding")
	}

	resp, err := p.Client.Do(upReq)
	if err != nil {
		attrs := []any{
			"req_id", reqID,
			"method", c.Request.Method,
			"upstream_scheme", upReq.URL.Scheme,
			"upstream_host", upReq.URL.Host,
			"body_replayable", bodyReplayable,
			"phase", trace.phase(),
			"category", classifyUpstreamError(c.Request.Context(), err),
			"error", err,
		}
		attrs = append(attrs, trace.attrs()...)
		p.Log.Error("上游请求失败", attrs...)
		p.writeError(c, http.StatusBadGateway, errors.New("上游请求失败"))
		return
	}
	defer resp.Body.Close()

	metrics.Inc()

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	isSSE := strings.HasPrefix(contentType, "text/event-stream")

	attrs := []any{"req_id", reqID, "status", resp.StatusCode, "sse", isSSE}
	attrs = append(attrs, trace.attrs()...)
	if resp.StatusCode >= http.StatusInternalServerError {
		p.Log.Warn("上游返回错误状态", attrs...)
	} else {
		p.Log.Debug("上游响应", attrs...)
	}

	dstHeader := c.Writer.Header()
	maps.Copy(dstHeader, resp.Header)
	removeHopByHopHeaders(dstHeader)
	if isSSE {
		c.Writer.Header().Del("Content-Length")
		c.Writer.Header().Del("Transfer-Encoding")
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
	}
	c.Status(resp.StatusCode)
	if f, ok := c.Writer.(http.Flusher); ok {
		f.Flush()
	}

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
			if flusher != nil {
				flusher.Flush()
			}
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

func HelloPage(c *gin.Context) {
	count := metrics.Total()
	qps := metrics.QPS()
	qpm := metrics.QPM()

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if xf := c.GetHeader("X-Forwarded-Proto"); xf != "" {
		scheme = strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	host := c.Request.Host
	if xfh := c.GetHeader("X-Forwarded-Host"); xfh != "" {
		host = strings.TrimSpace(strings.Split(xfh, ",")[0])
	}
	base := scheme + "://" + host

	str := fmt.Sprintf("AnyProxy 正在运行...\n累计转发: %d\n当前QPS: %d\n最近1分钟QPM: %d", count, qps, qpm)
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
