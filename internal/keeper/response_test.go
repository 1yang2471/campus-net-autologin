package keeper

import (
	"strings"
	"testing"
)

// 登录响应判定（Python parse_login 同语义）：标准 JSON / 伪 JSON /
// ret_code 型 / 纯文本多形态；异校只换词表。默认词表与 Python
// RESPONSE_DEFAULTS 逐字同源，本文件就是那份语料的 Go 侧对照。

func TestJudgeLoginResponseDefaults(t *testing.T) {
	m := DefaultResponseMarks()
	cases := []struct {
		name    string
		resp    string
		wantOK  bool
		wantSub string // 成功：期望原文包含；失败：期望错误文本包含
	}{
		{"JSON success", `{"success":true,"msg":"ok"}`, true, `"success":true`},
		{"伪 JSON success", "{'success':true, 'msg':'logon success'}", true, "logon success"},
		{"success 字符串 true", `{"success":"true"}`, true, `"success":"true"`},
		{"success 字符串 1", `{"success":"1"}`, true, ""},
		{"已在线哨兵", `{"success":false,"msg":"您已在线，请勿重复登录"}`, true, "已在线"},
		{"负向形态不算已在线", `{"success":false,"msg":"账号不在线，请勿重复登录"}`, false, "不在线"},
		{"密码错误硬拒绝", `{"success":false,"msg":"密码错误"}`, false, "密码错误"},
		{"ret_code 0", `{"ret_code":0,"msg":""}`, true, ""},
		{"ret_code + 成功词", `{"ret_code":1,"msg":"登录成功"}`, true, ""},
		{"ret_code 拒绝", `{"ret_code":1,"msg":"账号不存在"}`, false, "账号不存在"},
		{"瞬态 noauth", `{'success':false,'msg':"Task 'NOAUTH:*' not taken"}`, false, "not taken"},
		{"纯文本成功词", "登录成功", true, "登录成功"},
		{"纯文本 login_ok", "login_ok=true", true, "login_ok=true"},
		{"纯文本 login_ok false", "login_ok=false", false, "login_ok=false"},
		{"纯文本负向词", "密码错误", false, "密码错误"},
		{"英文 success", "success", true, "success"},
		{"空响应", "   ", false, "空响应"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := JudgeLoginResponse(tc.resp, m)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("应判成功，实际 %v", err)
				}
				if tc.wantSub != "" && !strings.Contains(out, tc.wantSub) {
					t.Errorf("成功原文应保留 %q，实际 %q", tc.wantSub, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("应判失败，实际成功原文 %q", out)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("错误应含 %q，实际 %q", tc.wantSub, err.Error())
			}
		})
	}
}

// 行为：长 HTML 里夹带 "success":false 片段不得让方言解析器冒充门户应答；
// 但华为伪 JSON 形态（'success':false + 'msg'）即使包在长文本里仍按结构判定。
func TestJudgeLongHTMLWithSuccessFragment(t *testing.T) {
	html := strings.Repeat("<div>页面</div>", 40) + ` "success":false, "msg":"请在页面重试"`
	_, err := JudgeLoginResponse(html, DefaultResponseMarks())
	if err == nil || !strings.Contains(err.Error(), "无法判定") {
		t.Fatalf("长 HTML 的 success 片段应归无法判定，实际 %v", err)
	}

	wrapped := strings.Repeat("<div>页面</div>", 40) +
		"<script>var r = {'success':false, 'msg':'密码错误'};</script>"
	_, err = JudgeLoginResponse(wrapped, DefaultResponseMarks())
	if err == nil || !strings.Contains(err.Error(), "密码错误") {
		t.Errorf("华为伪 JSON 形态应提 msg 判拒绝，实际 %v", err)
	}
}

// 行为：短中文响应按码点计长（Python len 口径），不能因 UTF-8 字节多被
// 误归「长响应」让负向词护栏失效。
func TestJudgeShortChineseResponseCountsRunes(t *testing.T) {
	resp := strings.Repeat("测", 80) + "密码错误" // 84 码点 / 252 字节
	_, err := JudgeLoginResponse(resp, DefaultResponseMarks())
	if err == nil || !strings.Contains(err.Error(), "密码错误") {
		t.Fatalf("短中文响应的负向词应生效，实际 %v", err)
	}
}

// 行为：长响应（HTML 门户页）里的负向词不得触发硬拒绝——只认强正向标记，
// 判不了归「无法判定」→ 瞬态短退避（Python trouble-maker P1-9 护栏）。
func TestJudgeLongResponseNegativeWordsIgnored(t *testing.T) {
	long := strings.Repeat("<div>错误提示 onerror</div>", 20)
	_, err := JudgeLoginResponse(long, DefaultResponseMarks())
	if err == nil || !strings.Contains(err.Error(), "无法判定") {
		t.Fatalf("长响应无强标记应归无法判定，实际 %v", err)
	}
	if strings.Contains(err.Error(), "<div>") {
		t.Errorf("无法判定不得把整段 HTML 当详情倾倒，实际 %q", err.Error())
	}
}

// 长响应里的强正向标记仍可判成功（login_ok=true 门户页）。
func TestJudgeLongResponseStrongPositive(t *testing.T) {
	long := strings.Repeat("x", 300) + " login_ok=true"
	if _, err := JudgeLoginResponse(long, DefaultResponseMarks()); err != nil {
		t.Errorf("长响应强正向标记应判成功，实际 %v", err)
	}
}

// 行为：异校词表覆盖生效，未覆盖的字段仍回落默认（Python _marks 同语义）。
func TestJudgeCustomMarksFallBackPerField(t *testing.T) {
	m := ResponseMarks{Success: SplitMarks("OK,搞定")}
	if _, err := JudgeLoginResponse("搞定", m); err != nil {
		t.Errorf("自定义成功词应生效: %v", err)
	}
	if _, err := JudgeLoginResponse("denied", m); err == nil {
		t.Error("未覆盖 negative_marks 时应回落默认（denied 不含默认词）")
	}
	if _, err := JudgeLoginResponse("失败", m); err == nil {
		t.Error("negative_marks 应回落默认词表")
	}
	if !m.withDefaults().transientMarked("请求异常") {
		t.Error("transient_marks 应回落默认词表")
	}
}

// 行为：默认词表与 Python RESPONSE_DEFAULTS 的关键词逐字对齐（异校适配文档
// 承诺的默认值不能漂移）。
func TestDefaultMarksMatchPython(t *testing.T) {
	m := DefaultResponseMarks()
	for _, w := range []string{"成功", "login_ok"} {
		if !m.successMarked(w) {
			t.Errorf("success_marks 缺 %q", w)
		}
	}
	for _, w := range []string{"noauth", "not taken", "请求异常", "空响应", "timeout", "无法判定"} {
		if !m.transientMarked(w) {
			t.Errorf("transient_marks 缺 %q", w)
		}
	}
	for _, w := range []string{"冻结", "freeze"} {
		if !m.freezeMarked(w) {
			t.Errorf("freeze_marks 缺 %q", w)
		}
	}
}
