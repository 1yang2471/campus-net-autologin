package clashapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRejectsNonLoopbackController(t *testing.T) {
	for _, raw := range []string{
		"http://example.com:9090",
		"https://127.0.0.1:9090",
		"http://user@127.0.0.1:9090",
		"http://127.0.0.1:9090/?x=1",
		"ftp://127.0.0.1:9090",
	} {
		if _, err := New(raw, ""); err == nil {
			t.Fatalf("应拒绝非安全控制器地址 %q", raw)
		}
	}
	if c, err := New("", ""); err != nil || c != nil {
		t.Fatalf("空配置应禁用且不报错，实际 %v %v", c, err)
	}
}

func TestFlushFakeIPUsesMihomoEndpointAndBearer(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	var method, path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, err := New(srv.URL, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.FlushFakeIP(context.Background()); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/cache/fakeip/flush" || auth != "Bearer secret" {
		t.Fatalf("请求形状错误: %s %s %s", method, path, auth)
	}
}

func TestFlushFakeIPRejectsNonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c, err := New(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.FlushFakeIP(context.Background()); err == nil {
		t.Fatal("非 204 必须报错")
	}
}
