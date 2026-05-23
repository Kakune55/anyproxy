package proxy

import (
	"errors"
	"net/url"
	"strings"
)

func normalizeURL(rawURL string) string {
	if strings.HasPrefix(rawURL, "https:/") && !strings.HasPrefix(rawURL, "https://") {
		return strings.Replace(rawURL, "https:/", "https://", 1)
	}
	if strings.HasPrefix(rawURL, "http:/") && !strings.HasPrefix(rawURL, "http://") {
		return strings.Replace(rawURL, "http:/", "http://", 1)
	}
	return rawURL
}

func BuildFromProxyPath(pathPart string, originalQuery url.Values) (string, error) {
	pathPart = strings.TrimPrefix(pathPart, "/")
	if pathPart == "" {
		return "", errors.New("目标为空")
	}
	pathPart = normalizeURL(pathPart)
	return mergeQuery(pathPart, originalQuery)
}

func BuildFromProtocol(protocol, remainder string, originalQuery url.Values) (string, error) {
	if protocol != "http" && protocol != "https" {
		return "", errors.New("不支持的协议")
	}
	full := protocol + ":/" + remainder
	full = normalizeURL(full)
	return mergeQuery(full, originalQuery)
}

func mergeQuery(raw string, original url.Values) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("不支持的协议")
	}
	if parsed.Host == "" {
		return "", errors.New("目标地址无效")
	}
	q := parsed.Query()
	for k, vs := range original {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}
