package netstack

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// G6 探锚判据需要状态码：3xx 劫持跳转、200 指纹劫持页、204 真在线——
// 纯 body 判据分不出来（Python physical_http 不跟随跳转返回首响应状态）。

// 行为：GetStatus 返回首响应状态码，不跟随跳转（跟随会把劫持 302 读成 200）。
func TestGetStatusNoRedirectFollow(t *testing.T) {
	redirected := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generate_204" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		redirected++
		http.Redirect(w, r, "/generate_204", http.StatusFound)
	}))
	defer srv.Close()

	c := New(net.IPv4(127, 0, 0, 1), 2*time.Second)
	st, _, err := c.GetStatus(srv.URL+"/go", nil)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if st != http.StatusFound {
		t.Errorf("必须返回首响应 302 而非跟随后的 204，实际 %d", st)
	}
	if redirected != 1 {
		t.Errorf("不得重复请求，实际 %d 次", redirected)
	}
}

// 行为：headers 里的 Host 生效（Go 客户端 Host 必须走 req.Host，
// Header.Set("Host") 会被静默忽略——Python 守护探锚靠 Host 覆盖）。
func TestHostHeaderOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Host))
	}))
	defer srv.Close()

	c := New(net.IPv4(127, 0, 0, 1), 2*time.Second)
	body, err := c.Get(srv.URL+"/x", map[string]string{"Host": "connect.rom.miui.com"}, "")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if body != "connect.rom.miui.com" {
		t.Errorf("Host 覆盖应生效，实际 %q", body)
	}
}
