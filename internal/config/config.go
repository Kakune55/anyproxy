package config

import (
	"flag"
	"fmt"
)

// Config 保存程序配置
type Config struct {
	Port          int
	Debug         bool
	LogFile       string
	ShutdownGrace int // 优雅停机等待秒数
	RequestTimeout int // 上游整体请求超时时间（秒）
}

// Parse 解析命令行参数返回配置
func Parse() *Config {
	cfg := &Config{}
	flag.IntVar(&cfg.Port, "port", 8080, "代理服务器监听端口")
	flag.BoolVar(&cfg.Debug, "debug", false, "调试模式 (debug level log)")
	flag.StringVar(&cfg.LogFile, "log", "", "日志文件路径 (默认输出到 stderr)")
	flag.IntVar(&cfg.ShutdownGrace, "grace", 10, "优雅停机等待秒数")
	flag.IntVar(&cfg.RequestTimeout, "timeout", 0, "单次上游请求超时秒(0=不设置)")
	flag.Parse()
	return cfg
}

func (c *Config) Addr() string { return fmt.Sprintf(":%d", c.Port) }
