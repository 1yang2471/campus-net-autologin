package app

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cnal/internal/clashapi"
)

func TestPlumbingWiresClashFlushAfterSuccess(t *testing.T) {
	SetPhysicalSources(PhysicalSources{V4: net.ParseIP("127.0.0.1")})
	t.Cleanup(func() {
		SetPhysicalSources(PhysicalSources{})
		RegisterInjectedPlumbing(nil)
	})
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method != http.MethodPost || r.URL.Path != "/cache/fakeip/flush" {
			t.Errorf("Clash 请求形状错误: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	cfg := defaultAdvancedConfig()
	cfg.Server = "https://portal.example"
	cfg.ClashAPIURL = srv.URL
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if pl.action.AfterSuccess == nil {
		t.Fatal("登录成功回调未接线")
	}
	pl.action.AfterSuccess()
	if hits != 1 {
		t.Fatalf("登录成功应刷新一次 fake-ip，实际 %d 次", hits)
	}
}

func TestPlumbingRejectsNonLoopbackClashURL(t *testing.T) {
	SetPhysicalSources(PhysicalSources{V4: net.ParseIP("127.0.0.1")})
	t.Cleanup(func() {
		SetPhysicalSources(PhysicalSources{})
		RegisterInjectedPlumbing(nil)
	})
	cfg := defaultAdvancedConfig()
	cfg.Server = "https://portal.example"
	cfg.ClashAPIURL = "http://example.com:9090"
	if _, err := newPlumbing(cfg, "u", "p"); err == nil || !strings.Contains(err.Error(), "Clash API 配置错误") {
		t.Fatalf("非回环 Clash 地址应拒绝启动，实际 %v", err)
	}
}

func TestPlumbingClashFlushFailureOnlyLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client, err := clashapi.New(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	pl := &plumbing{clash: client, logf: func(format string, args ...any) {
		logs.WriteString(fmt.Sprintf(format, args...))
	}}
	pl.flushClash()
	if !strings.Contains(logs.String(), "不影响认证") {
		t.Fatalf("失败应记录非阻断告警，实际 %q", logs.String())
	}
}
