# AnyProxy 简介

AnyProxy 是一个简单的 HTTP/HTTPS 代理服务器。它可以帮助你转发和代理请求。
支持 GET / POST / PUT / DELETE / HEAD / OPTIONS 请求。
兼容 SSE 流式请求。

## 使用方法

1. **直接协议路径**

   - 目标 URL: `https://example.com/path`  
      代理 URL: `http://AnyproxyIP/https/example.com/path`
   - 目标 URL: `http://example.com/path`  
      代理 URL: `http://AnyproxyIP/http/example.com/path`

2. **完整 URL 路径**
   - 目标 URL: `https://example.com`  
      代理 URL: `http://AnyproxyIP/proxy/https://example.com`

> 目标 URL 必须以 `https://` 或 `http://` 开头。

> 访问根路径可以查看使用方式

## 安装

1. 下载对应平台的二进制 Relase 文件
2. 运行二进制文件
3. (可选) 配置为系统服务

### 系统服务参考(Systemd)

```ini
# /etc/systemd/system/anyproxy.service
[Unit]
Description=AnyProxy Service
After=network.target

[Service]
ExecStart=/opt/anyproxy/anyproxy
WorkingDirectory=/opt/anyproxy
Restart=always
User=root

[Install]
WantedBy=multi-user.target
```

## 可选参数

| 参数     | 是否可选 |      默认值       | 数据类型 | 解释                              |
| -------- | -------: | :---------------: | -------- | --------------------------------- |
| -port    |       是 |       8080        | int      | 代理服务器监听端口                |
| -debug   |       是 |       false       | bool     | 调试模式（debug 级别日志）        |
| -log     |       是 | （输出到 stderr） | string   | 日志文件路径（默认输出到 stderr） |
| -grace   |       是 |        10         | int      | 优雅停机等待秒数                  |
| -timeout |       是 |         0         | int      | 单次上游请求超时秒（0 = 不设置）  |
