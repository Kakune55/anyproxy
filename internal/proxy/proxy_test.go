package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"anyproxy/internal/middleware"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestHandler(client *http.Client) http.Handler {
	p := New(client, slog.New(slog.NewTextHandler(io.Discard, nil)), DefaultReplayBodyLimitBytes)
	return middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/proxy/") {
			http.NotFound(w, r)
			return
		}
		p.HandleProxyPath(w, r, strings.TrimPrefix(r.URL.Path, "/proxy/"))
	}))
}

func TestProxyGeneratedErrorIncludesSource(t *testing.T) {
	h := newTestHandler(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, io.ErrUnexpectedEOF
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/https://example.com/test", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["source"] != "anyproxy" {
		t.Fatalf("source = %v, want anyproxy", body["source"])
	}
	if body["error"] != "上游请求失败" {
		t.Fatalf("error = %v, want 上游请求失败", body["error"])
	}
	if _, ok := body["req_id"]; !ok {
		t.Fatal("req_id missing")
	}
}

func TestUpstream5xxIsProxiedWithoutAnyProxySource(t *testing.T) {
	h := newTestHandler(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":"upstream"}`)),
			}, nil
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/https://example.com/test", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if strings.Contains(rec.Body.String(), "anyproxy") {
		t.Fatalf("proxied upstream response should not contain anyproxy source: %s", rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != `{"error":"upstream"}` {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestHopByHopHeadersAreNotForwarded(t *testing.T) {
	var upstreamReqHeader http.Header
	h := newTestHandler(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			upstreamReqHeader = req.Header.Clone()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Connection":        {"X-Remove-Me"},
					"Keep-Alive":        {"timeout=5"},
					"Transfer-Encoding": {"chunked"},
					"X-Remove-Me":       {"bad"},
					"X-Keep-Me":         {"ok"},
				},
				Body: io.NopCloser(strings.NewReader("ok")),
			}, nil
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/https://example.com/test", nil)
	req.Header.Set("Connection", "X-Client-Remove")
	req.Header.Set("X-Client-Remove", "bad")
	req.Header.Set("Proxy-Connection", "keep-alive")
	req.Header.Set("X-Keep-Me", "ok")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := upstreamReqHeader.Get("X-Client-Remove"); got != "" {
		t.Fatalf("request hop-by-hop extension forwarded: %q", got)
	}
	if got := upstreamReqHeader.Get("Proxy-Connection"); got != "" {
		t.Fatalf("Proxy-Connection forwarded: %q", got)
	}
	if got := upstreamReqHeader.Get("X-Keep-Me"); got != "ok" {
		t.Fatalf("normal request header = %q, want ok", got)
	}
	if got := rec.Header().Get("X-Remove-Me"); got != "" {
		t.Fatalf("response hop-by-hop extension forwarded: %q", got)
	}
	if got := rec.Header().Get("Keep-Alive"); got != "" {
		t.Fatalf("Keep-Alive forwarded: %q", got)
	}
	if got := rec.Header().Get("X-Keep-Me"); got != "ok" {
		t.Fatalf("normal response header = %q, want ok", got)
	}
}

func TestPrepareUpstreamBodySmallBodyIsReplayable(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))

	body, getBody, replayable, contentLength, err := prepareUpstreamBody(req, DefaultReplayBodyLimitBytes)
	if err != nil {
		t.Fatalf("prepare body: %v", err)
	}

	if !replayable {
		t.Fatal("small body should be replayable")
	}
	if getBody == nil {
		t.Fatal("GetBody missing")
	}
	if contentLength != 5 {
		t.Fatalf("contentLength = %d, want 5", contentLength)
	}
	assertReadAll(t, body, "hello")
	replay, err := getBody()
	if err != nil {
		t.Fatalf("GetBody: %v", err)
	}
	assertReadAll(t, replay, "hello")
}

func TestPrepareUpstreamBodyLargeKnownLengthStaysStreaming(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(bytes.NewReader([]byte("stream"))))
	req.ContentLength = DefaultReplayBodyLimitBytes + 1

	body, getBody, replayable, contentLength, err := prepareUpstreamBody(req, DefaultReplayBodyLimitBytes)
	if err != nil {
		t.Fatalf("prepare body: %v", err)
	}

	if replayable {
		t.Fatal("large body should not be replayable")
	}
	if getBody != nil {
		t.Fatal("large body should not have GetBody")
	}
	if contentLength != DefaultReplayBodyLimitBytes+1 {
		t.Fatalf("contentLength = %d, want %d", contentLength, DefaultReplayBodyLimitBytes+1)
	}
	assertReadAll(t, body, "stream")
}

func TestPrepareUpstreamBodyDisabledReplayStaysStreaming(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))

	body, getBody, replayable, contentLength, err := prepareUpstreamBody(req, 0)
	if err != nil {
		t.Fatalf("prepare body: %v", err)
	}

	if replayable {
		t.Fatal("body should not be replayable when replay limit is disabled")
	}
	if getBody != nil {
		t.Fatal("disabled replay should not have GetBody")
	}
	if contentLength != 5 {
		t.Fatalf("contentLength = %d, want 5", contentLength)
	}
	assertReadAll(t, body, "hello")
}

func assertReadAll(t *testing.T, r io.ReadCloser, want string) {
	t.Helper()
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
