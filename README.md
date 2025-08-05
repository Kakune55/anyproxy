# AnyProxy 简介

AnyProxy 是一个简单的 HTTP/HTTPS 代理服务器。它可以帮助你转发和代理请求。

## 使用方法

1. **直接协议路径**  
    - 目标URL: `https://example.com/path`  
        代理URL: `http://AnyproxyIP/https/example.com/path`
    - 目标URL: `http://example.com/path`  
        代理URL: `http://AnyproxyIP/http/example.com/path`

2. **完整URL路径**  
    - 目标URL: `https://example.com`  
        代理URL: `http://AnyproxyIP/proxy/https://example.com`

> 目标URL 必须以 `https://` 或 `http://` 开头。

## 安装

1. 下载对应平台的二进制Relase文件
2. 运行二进制文件
3. (可选) 配置为系统服务

系统服务参考(Systemd)
~~~ ini
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
~~~
