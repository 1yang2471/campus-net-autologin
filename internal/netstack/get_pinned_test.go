package netstack

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// GET 型登录端点（Dr.COM/Srun 明文族）：参数必须进 query，不能进 body。
// 底层原理：HTTP 方法只改请求行，urlencoded 的编码规则两族相同；GET 把
// form 拼到 query、body 为空，PostPinned 保持 body 不变——两条路共用
// 同一套绑源/钉 IP/Host 头纪律，不为 GET 开第二条实现。
func TestGetPinnedPutsFormInQueryNotBody(t *testing.T) {
	var gotQuery, gotBody, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotCT = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{'success':true}`))
	}))
	defer srv.Close()

	c := New(net.IPv4(127, 0, 0, 1), 2*time.Second)
	form := map[string]string{"opr": "login", "user": "23test", "pwd": "pw123"}
	_, err := c.GetPinned(srv.URL+"/login.php", form, nil, "")
	if err != nil {
		t.Fatalf("GET 请求失败: %v", err)
	}
	if gotQuery == "" {
		t.Fatal("GET 型登录的参数必须在 query，实际为空")
	}
	for _, want := range []string{"opr=login", "user=23test", "pwd=pw123"} {
		if !contains(gotQuery, want) {
			t.Errorf("query 缺 %q，实际 %q", want, gotQuery)
		}
	}
	if gotBody != "" {
		t.Errorf("GET 的 body 应为空，实际 %q", gotBody)
	}
	if gotCT != "" {
		t.Errorf("GET 无 body 时不应设 form Content-Type，实际 %q", gotCT)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
