package keeper

import (
	"reflect"
	"testing"

	"cnal/internal/protocol"
)

// 登录表单模板接线：LoginAction 必须把配置里的 Form 传进 BuildLogin，
// 自动登录与 v6 补绑两条路径都不能退回默认字段名。

func TestLoginActionUsesConfiguredForm(t *testing.T) {
	f, err := protocol.ParseForm("opr=login&user={user}&pass={pass}&lang=zh")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	a := &LoginAction{
		User: "alice", Pass: "s3cret", Form: f,
		Deliver: func(form map[string]string) (string, error) {
			got = form
			return `{"success":true}`, nil
		},
	}
	if _, err := a.Do(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"opr": "login", "user": "alice", "pass": "s3cret", "lang": "zh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("自动登录应吃配置表单\n实际: %v\n期望: %v", got, want)
	}
}

func TestDoRebindUsesConfiguredForm(t *testing.T) {
	f, err := protocol.ParseForm("opr=login&user={user}&pass={pass}")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true, Form: f,
		Handshake: func() (string, error) {
			return "{'success':true, 'ip':'2001:db8::1', 'isOnline':false}", nil
		},
		Deliver: func(form map[string]string) (string, error) {
			got = form
			return `{"success":true}`, nil
		},
	}
	if err := a.DoRebind(); err != nil {
		t.Fatal(err)
	}
	if got["user"] != "u" || got["pass"] != "p" || got["ipv4or6"] != "2001:db8::1" {
		t.Errorf("补绑应吃配置表单并带 ipv4or6，实际 %v", got)
	}
}
