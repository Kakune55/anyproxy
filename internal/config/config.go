package config

import (
	"flag"
	"fmt"
)

type Config struct {
	Port               int
	Debug              bool
	LogFile            string
	ShutdownGrace      int
	RequestTimeout     int
	LogLevel           string
	LogSource          bool
	AccessLog          bool
	ReplayBodyLimitMiB int
}

func Parse() *Config {
	cfg := &Config{}
	flag.IntVar(&cfg.Port, "port", 8080, "代理服务器监听端口")
	flag.BoolVar(&cfg.Debug, "debug", false, "调试模式 (debug level log)")
	flag.StringVar(&cfg.LogFile, "log", "", "日志文件路径 (默认输出到 stderr)")
	flag.IntVar(&cfg.ShutdownGrace, "grace", 10, "优雅停机等待秒数")
	flag.IntVar(&cfg.RequestTimeout, "timeout", 0, "单次上游请求超时秒(0=不设置)")
	flag.StringVar(&cfg.LogLevel, "log-level", "warn", "日志等级: debug|info|warn|error (默认 warn)")
	flag.BoolVar(&cfg.LogSource, "log-source", false, "日志中记录源码位置")
	flag.BoolVar(&cfg.AccessLog, "access-log", true, "记录每个 HTTP 请求的访问日志")
	flag.IntVar(&cfg.ReplayBodyLimitMiB, "replay-body-limit", 8, "可重放上游请求体上限 MiB，用于 HTTP/2 自动重试(0=禁用)")
	flag.Parse()

	// 兼容旧的 -debug 参数: 当 -debug 为 true 且未显式指定其它日志等级(仍为默认 warn) 时，提升为 debug
	if cfg.Debug && cfg.LogLevel == "warn" {
		cfg.LogLevel = "debug"
	}
	return cfg
}

func (c *Config) Addr() string { return fmt.Sprintf(":%d", c.Port) }

func (c *Config) ReplayBodyLimitBytes() int64 {
	if c.ReplayBodyLimitMiB <= 0 {
		return 0
	}
	return int64(c.ReplayBodyLimitMiB) << 20
}
