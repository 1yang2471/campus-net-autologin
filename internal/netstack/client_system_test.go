package netstack

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 双路径的 B 路：不绑源，按系统默认路由/代理规则拨号（与浏览器同路）。
// 设计 2026-11-10-双路径自适应设计.md §3/§8 第一期第 1 项。

// 行为：系统栈客户端没有绑定源也能发请求——TUN 接管默认路由时，
// 门户域名只能走这条与用户上网同路的路。
func TestSystemClientUsesDefaultRoute(t *testing.T) {
	var seenRemote string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenRemote = r.RemoteAddr
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewSystem(2 * time.Second)
	body, err := c.Get(srv.URL, nil, "")
	if err != nil {
		t.Fatalf("系统栈客户端应能发请求（不绑源是它的存在理由）: %v", err)
	}
	if body != "ok" {
		t.Errorf("响应体应为 ok，实际 %q", body)
	}
	if !strings.HasPrefix(seenRemote, "127.0.0.1:") {
		t.Errorf("回环测试服务应看到本机源地址，实际 %q", seenRemote)
	}
}

// 行为：绑源纪律不因新增系统栈而松掉——New(nil) 仍是物理栈配置错误，
// 请求入口直接拒绝（fail-closed，绝不静默改成系统栈）。
func TestBoundClientStillRejectsNilSource(t *testing.T) {
	c := New(nil, time.Second)
	if _, err := c.Get("http://127.0.0.1:1/x", nil, ""); err == nil ||
		!strings.Contains(err.Error(), "未绑定源地址") {
		t.Errorf("绑源客户端缺源必须拒绝，实际 %v", err)
	}
	if _, _, err := c.GetStatus("http://127.0.0.1:1/x", nil); err == nil ||
		!strings.Contains(err.Error(), "未绑定源地址") {
		t.Errorf("GetStatus 同样必须拒绝，实际 %v", err)
	}
}

// 行为：系统栈客户端拒绝钉 IP——pin 是绑源路径的产物（来自绑源 DNS），
// 系统栈上带 pin 说明接线错了；静默忽略会把两条路的判据混在一起。
func TestSystemClientRejectsPinnedIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	c := NewSystem(time.Second)
	if _, err := c.Get(srv.URL, nil, "127.0.0.1:1"); err == nil ||
		!strings.Contains(err.Error(), "系统栈") {
		t.Errorf("系统栈 + pin 必须报错，实际 %v", err)
	}
	if _, _, err := c.GetStatusPinned(srv.URL, nil, "127.0.0.1:1"); err == nil ||
		!strings.Contains(err.Error(), "系统栈") {
		t.Errorf("GetStatusPinned 同样必须报错，实际 %v", err)
	}
}
