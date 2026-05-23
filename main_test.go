package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"anyproxy/internal/proxy"
)

func TestBuildHandlerRoutesProxyPath(t *testing.T) {
	var upstreamURL string
	h := newMainTestHandler(func(req *http.Request) (*http.Response, error) {
		upstreamURL = req.URL.String()
		return textResponse(http.StatusOK, "ok"), nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/https://example.com/path?q=1", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if upstreamURL != "https://example.com/path?q=1" {
		t.Fatalf("upstream URL = %q", upstreamURL)
	}
}

func TestBuildHandlerRoutesProtocolPath(t *testing.T) {
	var upstreamURL string
	h := newMainTestHandler(func(req *http.Request) (*http.Response, error) {
		upstreamURL = req.URL.String()
		return textResponse(http.StatusOK, "ok"), nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/https/example.com/chat", strings.NewReader(`{}`))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if upstreamURL != "https://example.com/chat" {
		t.Fatalf("upstream URL = %q", upstreamURL)
	}
}

func TestBuildHandlerServesRootAndMetrics(t *testing.T) {
	h := newMainTestHandler(func(*http.Request) (*http.Response, error) {
		t.Fatal("upstream should not be called")
		return nil, nil
	})

	for _, path := range []string{"/", "/metrics"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

func TestBuildHandlerNotFoundForUnknownSingleSegment(t *testing.T) {
	h := newMainTestHandler(func(*http.Request) (*http.Response, error) {
		t.Fatal("upstream should not be called")
		return nil, nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func newMainTestHandler(rt func(*http.Request) (*http.Response, error)) http.Handler {
	p := proxy.New(&http.Client{Transport: roundTripFunc(rt)}, slog.New(slog.NewTextHandler(io.Discard, nil)), proxy.DefaultReplayBodyLimitBytes)
	return buildHandler(p, slog.New(slog.NewTextHandler(io.Discard, nil)), false)
}

func textResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"text/plain"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
