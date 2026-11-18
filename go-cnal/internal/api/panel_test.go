package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// UI④web 面板伺服（选型 A：零进程零内存代价，浏览器吃回路契约）：
// GET /panel?token=… → 单页面（go:embed）；token 错 → 404（面板 URL 含
// token，等于把凭据放 URL——不给未持 token 者任何探测面）。
// Windows 浏览器与安卓 WebView 吃同一份页料（规格：UI 实际新增只有一份）。

// newPanelTestServer 起一个带面板路由的测试服务。
func newPanelTestServer(t *testing.T, page string) (*Server, *httptest.Server) {
	t.Helper()
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	s.PanelPage = page
	return s, ts
}

// 行为：带正确 token 打开面板 → 200 完整 HTML。
func TestPanelServedWithValidToken(t *testing.T) {
	s, ts := newPanelTestServer(t, "<html><body>cnal panel</body></html>")
	resp, err := http.Get(ts.URL + "/panel?token=" + s.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("带 token 应 200，实际 %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "cnal panel") {
		t.Errorf("应返回注入的面板页，实际 %q", truncStr(string(b), 60))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type 应为 text/html，实际 %q", ct)
	}
}

// 行为：错 token → 401（auth 拦在 panel 之前，同样不暴露面板存在）。
func TestPanelRejectsBadToken(t *testing.T) {
	_, ts := newPanelTestServer(t, "<html>x</html>")
	resp, err := http.Get(ts.URL + "/panel?token=wrong")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("错 token 应 401，实际 %d", resp.StatusCode)
	}
}

// 行为：未设置 PanelPage → /panel 返回 404（cnald 无头壳没有页料也不崩）。
func TestPanelAbsentWhenNoPage(t *testing.T) {
	s, ts := newPanelTestServer(t, "")
	resp, err := http.Get(ts.URL + "/panel?token=" + s.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("无页料应 404，实际 %d", resp.StatusCode)
	}
}

// 行为：面板页带安全头——CSP 限制外联、no-store 防 token 页落缓存、
// nosniff 防 MIME 猜测。页面本身是本地静态料，CSP 只放行 inline 与自身 fetch。
func TestPanelSendsSecurityHeaders(t *testing.T) {
	s, ts := newPanelTestServer(t, "<html>x</html>")
	resp, err := http.Get(ts.URL + "/panel?token=" + s.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'unsafe-inline'", "connect-src 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP 应含 %q，实际 %q", want, csp)
		}
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control 应为 no-store，实际 %q", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options 应为 nosniff，实际 %q", got)
	}
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
