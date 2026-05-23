package middleware

import (
	"encoding/json"
	"net/http"
	"time"

	"anyproxy/internal/metrics"
)

func MetricsHandler(w http.ResponseWriter, _ *http.Request) {
	qps := metrics.QPS()
	qpm := metrics.QPM()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"qps_current": qps,
		"qps_avg_60s": float64(qpm) / 60.0,
		"qpm_current": qpm,
		"total":       metrics.Total(),
		"timestamp":   time.Now().Unix(),
	})
}
