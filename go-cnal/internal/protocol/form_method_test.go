package protocol

import "testing"

// GET 型登录（Dr.COM/Srun 明文族）：login_form 支持可选方法前缀。
// 设计选择：方法写在模板第一行（"GET&…"），不进表单字段——表单字段原样发门户，
// 方法只是本地投递指令。默认 POST：老模板一字不改（兼容参考校与面板回显）。
func TestParseFormMethodPrefix(t *testing.T) {
	cases := []struct {
		tmpl       string
		wantMethod string
		wantFields int
	}{
		{"opr=pwdLogin&userName={user}&pwd={pass}", "POST", 3},
		{"GET&opr=login&user={user}&pass={pass}", "GET", 3},
		{"POST&opr=login&user={user}&pass={pass}", "POST", 3},
	}
	for _, c := range cases {
		f, err := ParseForm(c.tmpl)
		if err != nil {
			t.Fatalf("解析 %q: %v", c.tmpl, err)
		}
		if f.Method != c.wantMethod {
			t.Errorf("%q method 应为 %q，实际 %q", c.tmpl, c.wantMethod, f.Method)
		}
		if len(f.Fields("u", "p")) != c.wantFields {
			t.Errorf("%q 字段数应为 %d（方法前缀不进表单），实际 %v",
				c.tmpl, c.wantFields, f.Fields("u", "p"))
		}
	}
}

func TestParseFormRejectsBadMethod(t *testing.T) {
	for _, bad := range []string{
		"DELETE&a={user}&b={pass}",
		"get&a={user}&b={pass}", // 方法必须大写（避免大小写分裂）
	} {
		if _, err := ParseForm(bad); err == nil {
			t.Errorf("%q 非法方法应被拒", bad)
		}
	}
}

func TestFormTemplateKeepsMethod(t *testing.T) {
	f, err := ParseForm("GET&opr=login&user={user}&pass={pass}")
	if err != nil {
		t.Fatal(err)
	}
	if f.Template() != "GET&opr=login&user={user}&pass={pass}" {
		t.Errorf("Template 应回显方法前缀，实际 %q", f.Template())
	}
}
