package config

import "testing"

func TestReplayBodyLimitBytes(t *testing.T) {
	tests := []struct {
		name string
		mib  int
		want int64
	}{
		{name: "disabled", mib: 0, want: 0},
		{name: "negative disabled", mib: -1, want: 0},
		{name: "positive", mib: 8, want: 8 << 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{ReplayBodyLimitMiB: tt.mib}
			if got := cfg.ReplayBodyLimitBytes(); got != tt.want {
				t.Fatalf("ReplayBodyLimitBytes() = %d, want %d", got, tt.want)
			}
		})
	}
}
