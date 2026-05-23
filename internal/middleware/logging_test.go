package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggerDisabledDoesNotWriteAccessLog(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	handler := Logger(logger, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(rec, req)

	if logs.Len() != 0 {
		t.Fatalf("expected no access logs, got %q", logs.String())
	}
}

func TestLoggerWritesAccessLog(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	handler := RequestID(Logger(logger, true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test?q=1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	handler.ServeHTTP(rec, req)

	got := logs.String()
	for _, want := range []string{
		"HTTP请求",
		"method=POST",
		"path=\"/test?q=1\"",
		"status=201",
		"size=7",
		"ip=192.0.2.1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log %q does not contain %q", got, want)
		}
	}
}

func TestLoggerRecordsRecoveredPanicStatus(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	handler := RequestID(Logger(logger, true)(Recovery(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := logs.String(); !strings.Contains(got, "status=500") {
		t.Fatalf("log %q does not contain status=500", got)
	}
}
