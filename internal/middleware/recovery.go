package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
)

func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rcv := recover(); rcv != nil {
					logger.Error("发生Panic",
						"req_id", GetReqID(r),
						"error", rcv,
						"stack", string(debug.Stack()),
						"path", r.URL.Path,
					)
					writeJSON(w, http.StatusInternalServerError, map[string]any{
						"error":  "内部服务器错误",
						"req_id": GetReqID(r),
						"source": "anyproxy",
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
