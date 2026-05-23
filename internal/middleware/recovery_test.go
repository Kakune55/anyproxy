package middleware

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRecoveryErrorIncludesSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(), Recovery(slog.New(slog.NewTextHandler(io.Discard, nil))))
	r.GET("/panic", func(*gin.Context) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["source"] != "anyproxy" {
		t.Fatalf("source = %v, want anyproxy", body["source"])
	}
	if body["error"] != "内部服务器错误" {
		t.Fatalf("error = %v, want 内部服务器错误", body["error"])
	}
	if _, ok := body["req_id"]; !ok {
		t.Fatal("req_id missing")
	}
}
