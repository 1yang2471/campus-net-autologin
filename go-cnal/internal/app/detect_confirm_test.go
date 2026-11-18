package app

import (
	"errors"
	"strings"
	"testing"

	"cnal/internal/keeper"
)

// 行为确认（指纹快筛未命中后的第二路径）离线测试：注入发包替身，不触网、
// 不发真实凭据。成功 / 判负 / 发包失败三态钉死，保证与指纹路径互补而不改它。

// 发包缝替身记录实参，验证 method/url/form 原样透传、不被路径改写。
type recordingSend struct {
	method string
	url    string
	form   map[string]string
	reply  string
	err    error
}

func (s *recordingSend) send(method, loginURL string, form map[string]string) (string, error) {
	s.method, s.url, s.form = method, loginURL, form
	return s.reply, s.err
}

// (a) 响应含成功词 → 可用，证据说明通过；缝实参如实透传。
func TestConfirmPortalByLoginSuccessWord(t *testing.T) {
	s := &recordingSend{reply: `{"success":true,"msg":"ok"}`}
	ok, ev, err := ConfirmPortalByLogin(ConfirmOptions{
		LoginURL: "http://10.0.0.1/ac_portal/login.php",
		Method:   "POST",
		Form:     map[string]string{"opr": "pwdLogin", "userName": "fake_user", "pwd": "fake_pass"},
		Marks:    keeper.DefaultResponseMarks(),
		Send:     s.send,
	})
	if err != nil {
		t.Fatalf("成功词响应不应报错，实际 %v", err)
	}
	if !ok {
		t.Fatalf("成功词响应应判可用，证据 %q", ev)
	}
	if !strings.Contains(ev, "成功") {
		t.Errorf("证据应说明命中成功词表，实际 %q", ev)
	}
	if s.method != "POST" || s.url != "http://10.0.0.1/ac_portal/login.php" || s.form["pwd"] != "fake_pass" {
		t.Errorf("发包缝实参被改写：method=%q url=%q form=%v", s.method, s.url, s.form)
	}
}

// (b) 响应含失败词 → 不可用（但非系统错误），证据点明词表可能不匹配。
func TestConfirmPortalByLoginFailureWord(t *testing.T) {
	s := &recordingSend{reply: `{"success":false,"msg":"密码错误"}`}
	ok, ev, err := ConfirmPortalByLogin(ConfirmOptions{
		LoginURL: "http://10.0.0.1/ac_portal/login.php",
		Form:     map[string]string{"userName": "fake_user", "pwd": "fake_pass"},
		Marks:    keeper.DefaultResponseMarks(),
		Send:     s.send,
	})
	if err != nil {
		t.Fatalf("判负是有效结论，不应报错，实际 %v", err)
	}
	if ok {
		t.Fatalf("失败词响应应判不可用，证据 %q", ev)
	}
	if !strings.Contains(ev, "词表可能不匹配") {
		t.Errorf("证据应说明词表可能不匹配，实际 %q", ev)
	}
	if !strings.Contains(ev, "密码错误") {
		t.Errorf("证据应带上门户判负原因，实际 %q", ev)
	}
}

// (c) 发包失败 → err 上抛（与「判负」区分：这是系统错误，不是可用性结论）。
func TestConfirmPortalByLoginSendFailure(t *testing.T) {
	boom := errors.New("dial tcp: 网络不可达")
	s := &recordingSend{err: boom}
	ok, _, err := ConfirmPortalByLogin(ConfirmOptions{
		LoginURL: "http://10.0.0.1/ac_portal/login.php",
		Form:     map[string]string{"userName": "fake_user", "pwd": "fake_pass"},
		Marks:    keeper.DefaultResponseMarks(),
		Send:     s.send,
	})
	if ok {
		t.Error("发包失败不得判可用")
	}
	if !errors.Is(err, boom) {
		t.Errorf("发包错误应可 errors.Is 追到根因，实际 %v", err)
	}
}

// 缺省词表（Marks 零值）走 DefaultResponseMarks：成功词照样判正（与守护
// JudgeLoginResponse 同一回落语义），异校手造词表亦生效。
func TestConfirmPortalByLoginDefaultMarksAndGetMethod(t *testing.T) {
	s := &recordingSend{reply: "登录成功"}
	ok, _, err := ConfirmPortalByLogin(ConfirmOptions{
		LoginURL: "http://10.0.0.1/login",
		Method:   "get", // 小写也要归一成 GET 透传
		Form:     map[string]string{"user": "fake_user", "pass": "fake_pass"},
		Send:     s.send,
	})
	if err != nil || !ok {
		t.Fatalf("缺省词表下纯文本成功词应判可用，ok=%v err=%v", ok, err)
	}
	if s.method != "GET" {
		t.Errorf("method 应归一成大写 GET，实际 %q", s.method)
	}
}

// 手造词表（不用默认）也要生效：默认负向词里没有 denied，配置后应判负。
func TestConfirmPortalByLoginCustomMarks(t *testing.T) {
	s := &recordingSend{reply: "denied"}
	ok, ev, err := ConfirmPortalByLogin(ConfirmOptions{
		LoginURL: "http://10.0.0.1/login",
		Form:     map[string]string{"user": "fake_user", "pass": "fake_pass"},
		Marks:    keeper.ResponseMarks{Negative: []string{"denied"}},
		Send:     s.send,
	})
	if err != nil || ok {
		t.Fatalf("自定义负向词应判不可用，ok=%v err=%v", ok, err)
	}
	if !strings.Contains(ev, "词表可能不匹配") {
		t.Errorf("证据应说明词表可能不匹配，实际 %q", ev)
	}
}

// 缺发包缝是调用方接线错误，必须显式报错而不是 panic。
func TestConfirmPortalByLoginRequiresSend(t *testing.T) {
	if _, _, err := ConfirmPortalByLogin(ConfirmOptions{
		LoginURL: "http://10.0.0.1/login",
		Form:     map[string]string{"user": "fake"},
	}); err == nil {
		t.Error("Send 为空应报错")
	}
}
