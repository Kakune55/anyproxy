package middleware

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
)

type requestIDKey struct{}

var globalReqID atomic.Int64

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := globalReqID.Add(1)
		w.Header().Set("X-Request-ID", strconv.FormatInt(id, 10))
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func GetReqID(r *http.Request) int64 {
	if id, ok := r.Context().Value(requestIDKey{}).(int64); ok {
		return id
	}
	return 0
}
