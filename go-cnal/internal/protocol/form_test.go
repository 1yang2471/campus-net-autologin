package protocol

import (
	"reflect"
	"strings"
	"testing"
)

// 登录表单模板：ac_portal 族异校变体的字段名/操作名可配，但形状仍是
// 「静态键值 + {user}/{pass} 两个占位」。默认模板与 Python FORM_BASE /
// parse_login 逐字同形（参考校零改动）。

func TestDefaultFormMatchesPython(t *testing.T) {
	absent := NewPeerProvider(func() (string, error) { return "", ErrPeerAbsent })
	body := EncodeForm(BuildLogin("u", "p", Settings{}, absent))
	want := "opr=pwdLogin&pwd=p&rememberPwd=1&userName=u" // EncodeForm 按键排序
	if body != want {
		t.Errorf("默认表单必须与 Python 同形\n实际: %s\n期望: %s", body, want)
	}
	if got := DefaultForm().Template(); got != DefaultFormTemplate {
		t.Errorf("默认模板与 DefaultForm 漂移: %q vs %q", got, DefaultFormTemplate)
	}
}

func TestParseFormCustomTemplate(t *testing.T) {
	f, err := ParseForm("opr=login&user_account={user}&user_password={pass}&lang=zh")
	if err != nil {
		t.Fatal(err)
	}
	got := f.Fields("alice", "s3cret")
	want := map[string]string{
		"opr": "login", "user_account": "alice", "user_password": "s3cret", "lang": "zh",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("自定义模板填值错误\n实际: %v\n期望: %v", got, want)
	}
	if f.Template() != "opr=login&user_account={user}&user_password={pass}&lang=zh" {
		t.Errorf("模板应可往返，实际 %q", f.Template())
	}
}

func TestParseFormEmptyIsDefault(t *testing.T) {
	f, err := ParseForm("   ")
	if err != nil {
		t.Fatal(err)
	}
	if f.Template() != DefaultFormTemplate {
		t.Errorf("空模板应回落默认，实际 %q", f.Template())
	}
}

func TestParseFormRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"缺用户名占位": "opr=pwdLogin&pwd={pass}",
		"缺密码占位":  "opr=pwdLogin&userName={user}",
		"重复键":    "opr=a&opr=b&userName={user}&pwd={pass}",
		"未知占位":   "userName={account}&pwd={pass}",
		"占位嵌在字面量": "userName=x{user}x&pwd={pass}",
		"非法键":    "user name={user}&pwd={pass}",
		"空段":     "opr=pwdLogin&&userName={user}&pwd={pass}",
		"缺等号":    "opr=pwdLogin&userName&pwd={pass}",
		"保留键":    "ipv4or6=1&userName={user}&pwd={pass}",
	}
	for name, raw := range cases {
		if _, err := ParseForm(raw); err == nil {
			t.Errorf("%s 必须被拒绝: %q", name, raw)
		}
	}
}

func TestBuildLoginUsesCustomForm(t *testing.T) {
	f, err := ParseForm("opr=login&user={user}&pass={pass}")
	if err != nil {
		t.Fatal(err)
	}
	absent := NewPeerProvider(func() (string, error) { return "", ErrPeerAbsent })
	got := BuildLogin("u", "p", Settings{Form: f}, absent)
	want := map[string]string{"opr": "login", "user": "u", "pass": "p"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildLogin 应吃自定义模板\n实际: %v\n期望: %v", got, want)
	}
}

// 双栈字段由 BuildLogin 统一管理：模板里出现 ipv4or6 必须拒绝，否则
// 静态值与现场值谁覆盖谁没有明确语义。
func TestParseFormRejectsReservedDualStackKey(t *testing.T) {
	if _, err := ParseForm("userName={user}&pwd={pass}&ipv4or6=x"); err == nil ||
		!strings.Contains(err.Error(), "ipv4or6") {
		t.Errorf("保留键应带明确错误，实际 %v", err)
	}
}
