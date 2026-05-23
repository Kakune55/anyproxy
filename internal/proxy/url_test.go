package proxy

import (
	"net/url"
	"testing"
)

func TestBuildFromProxyPathNormalizesAndMergesQuery(t *testing.T) {
	got, err := BuildFromProxyPath("/https:/example.com/path?existing=1", url.Values{
		"q":        {"hello world"},
		"existing": {"2"},
	})
	if err != nil {
		t.Fatalf("BuildFromProxyPath: %v", err)
	}

	want := "https://example.com/path?existing=1&existing=2&q=hello+world"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestBuildFromProtocol(t *testing.T) {
	got, err := BuildFromProtocol("https", "/example.com/path", url.Values{"q": {"1"}})
	if err != nil {
		t.Fatalf("BuildFromProtocol: %v", err)
	}

	want := "https://example.com/path?q=1"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestBuildFromProtocolRejectsUnsupportedProtocol(t *testing.T) {
	if _, err := BuildFromProtocol("ftp", "/example.com/path", nil); err == nil {
		t.Fatal("expected unsupported protocol error")
	}
}

func TestBuildFromProxyPathRejectsEmptyTarget(t *testing.T) {
	if _, err := BuildFromProxyPath("/", nil); err == nil {
		t.Fatal("expected empty target error")
	}
}
