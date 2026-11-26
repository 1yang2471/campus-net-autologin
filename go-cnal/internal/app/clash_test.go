package app

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cnal/internal/clashapi"
)

func TestPlumbingWiresClashFlushAfterSuccess(t *testing.T) {
	SetPhysicalSources(PhysicalSources{V4: net.ParseIP("127.0.0.1")})
	t.Cleanup(func() {
		SetPhysicalSources(PhysicalSources{})
		RegisterInjectedPlumbing(nil)
	})
	hitCh := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/cache/fakeip/flush" {
			t.Errorf("Clash 请求形状错误: %s %s", r.Method, r.URL.Path)
		}
		hitCh <- struct{}{}
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
	select {
	case <-hitCh:
	case <-time.After(2 * time.Second):
		t.Fatal("登录成功应异步刷新 fake-ip")
	}
}

func TestPlumbingInvalidClashURLDisablesIntegration(t *testing.T) {
	SetPhysicalSources(PhysicalSources{V4: net.ParseIP("127.0.0.1")})
	t.Cleanup(func() {
		SetPhysicalSources(PhysicalSources{})
		RegisterInjectedPlumbing(nil)
	})
	cfg := defaultAdvancedConfig()
	cfg.Server = "https://portal.example"
	cfg.ClashAPIURL = "http://example.com:9090"
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatalf("非法 Clash 配置不得阻断认证启动: %v", err)
	}
	if pl.clash != nil {
		t.Fatal("非法 Clash 配置应禁用联动")
	}
}

func TestPlumbingClashFlushCoalescesPending(t *testing.T) {
	hits := make(chan int, 2)
	release := make(chan struct{})
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count++
		hits <- count
		if count == 1 {
			<-release
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	client, err := clashapi.New(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	pl := &plumbing{clash: client}
	pl.flushClash()
	if first := <-hits; first != 1 {
		t.Fatalf("首次 flush 编号错误: %d", first)
	}
	pl.flushClash()
	pl.flushClash()
	close(release)
	select {
	case second := <-hits:
		if second != 2 {
			t.Fatalf("pending 应合并为一次追加 flush，实际编号 %d", second)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending flush 未执行")
	}
	select {
	case extra := <-hits:
		t.Fatalf("多次触发不得无限放大，实际额外 %d", extra)
	case <-time.After(100 * time.Millisecond):
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
	logCh := make(chan string, 1)
	pl := &plumbing{clash: client, logf: func(format string, args ...any) {
		logCh <- fmt.Sprintf(format, args...)
	}}
	pl.flushClash()
	select {
	case msg := <-logCh:
		if !strings.Contains(msg, "不影响认证") {
			t.Fatalf("失败应记录非阻断告警，实际 %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("flush 失败应异步记录告警")
	}
}
