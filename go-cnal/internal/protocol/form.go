package protocol

import (
	"errors"
	"fmt"
	"strings"
)

// 登录表单模板：ac_portal 族的端点/字段名是校事实，字段名并不总是参考校的
// userName/pwd。模板把「形状」变成配置：静态键值 + 恰好一个 {user} 与一个
// {pass} 占位。默认模板与 Python FORM_BASE / parse_login 逐字同形——
// 参考校零改动；异校变体只改 ini 的 login_form，不改代码。
//
// 不做任意表达式/动态 DSL（规格 §6 否决）：值只有字面量与两个凭据占位，
// 解析结果仍是普通表单，编码走 EncodeForm（URL 转义，不拼字符串）。

// DefaultFormTemplate 参考校默认模板（唯一出处；DefaultForm 由它解析）。
const DefaultFormTemplate = "opr=pwdLogin&userName={user}&pwd={pass}&rememberPwd=1"

// reservedFormKey 由双栈开关统一管理，模板不得静态覆盖（否则静态值与现场
// 值的覆盖顺序没有明确语义）。
const reservedFormKey = "ipv4or6"

type formField struct {
	key   string
	value string // 字面量，或 {user} / {pass}
}

// Form 解析后的登录表单模板（不可变；零值 = 默认模板）。
type Form struct {
	fields []formField
}

// DefaultForm 返回参考校默认模板。字段字面量与 DefaultFormTemplate 同源，
// 由 TestDefaultFormMatchesPython 守着（不在此再解析一次，避免初始化环）。
func DefaultForm() Form {
	return Form{fields: []formField{
		{key: "opr", value: "pwdLogin"},
		{key: "userName", value: "{user}"},
		{key: "pwd", value: "{pass}"},
		{key: "rememberPwd", value: "1"},
	}}
}

// ParseForm 解析模板。空串 = 默认模板（面板留空不改变行为）；其余必须
// 恰好含一个 {user} 与一个 {pass}，键不得重复、不得含分隔符/空白。
func ParseForm(s string) (Form, error) {
	if strings.TrimSpace(s) == "" {
		return DefaultForm(), nil
	}
	var fields []formField
	seen := map[string]bool{}
	userN, passN := 0, 0
	for _, seg := range strings.Split(s, "&") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			return Form{}, errors.New("登录表单模板含空段")
		}
		k, v, ok := strings.Cut(seg, "=")
		if !ok {
			return Form{}, fmt.Errorf("登录表单段 %q 缺少 =", seg)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "" {
			return Form{}, errors.New("登录表单含空键")
		}
		if strings.ContainsAny(k, " \t\r\n&={}%") {
			return Form{}, fmt.Errorf("登录表单键 %q 含非法字符", k)
		}
		if k == reservedFormKey {
			return Form{}, fmt.Errorf("登录表单不得包含保留键 %s（由 dual_stack 管理）", reservedFormKey)
		}
		if seen[k] {
			return Form{}, fmt.Errorf("登录表单键 %q 重复", k)
		}
		seen[k] = true
		switch v {
		case "{user}":
			userN++
		case "{pass}":
			passN++
		default:
			if strings.ContainsAny(v, "{}%") {
				// % 拒绝：EncodeForm 会再做 URL 编码，devtools 里抄来的
				// percent-encoded 值（next=%2F…）会变成二次编码静默发错。
				return Form{}, fmt.Errorf(
					"登录表单值 %q 含 %% 或占位符——%% 会被二次编码，请写未转义原文（/ 而非 %%2F）", v)
			}
		}
		fields = append(fields, formField{key: k, value: v})
	}
	if userN != 1 {
		return Form{}, fmt.Errorf("登录表单必须恰好包含一个 {user}，实际 %d 个", userN)
	}
	if passN != 1 {
		return Form{}, fmt.Errorf("登录表单必须恰好包含一个 {pass}，实际 %d 个", passN)
	}
	return Form{fields: fields}, nil
}

// Template 渲染回模板字符串（面板回显 / 日志诊断用；键序保持解析顺序）。
func (f Form) Template() string {
	if len(f.fields) == 0 {
		return DefaultFormTemplate
	}
	parts := make([]string, len(f.fields))
	for i, x := range f.fields {
		parts[i] = x.key + "=" + x.value
	}
	return strings.Join(parts, "&")
}

// HasV6Bind 报告表单是否携带了保留键 ipv4or6。登录方要判断「这次到底带没
// 带上 v6」只能从表单本身读——BuildLogin 的降级是「不带字段」，不报错也不
// 留痕（2026-11-09 切账号事故）；键名的第二处硬写必然漂移。
func HasV6Bind(form map[string]string) bool {
	_, ok := form[reservedFormKey]
	return ok
}

// Fields 用当前凭据填模板（每次登录现取，凭据不缓存）。
func (f Form) Fields(user, pass string) map[string]string {
	if len(f.fields) == 0 {
		f = DefaultForm()
	}
	out := make(map[string]string, len(f.fields))
	for _, x := range f.fields {
		switch x.value {
		case "{user}":
			out[x.key] = user
		case "{pass}":
			out[x.key] = pass
		default:
			out[x.key] = x.value
		}
	}
	return out
}
