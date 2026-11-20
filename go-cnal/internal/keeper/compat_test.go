package keeper

import (
	"strings"
	"testing"
)

// 离线兼容性自检（与守护同语义）：喂一段登录响应/登录页
// 样本，不联网、不发凭据，回答「我校门户的响应会被判成什么」。测试只描述
// 行为，不假设实现放在哪个文件。

func TestCompatCheckSuccessJSON(t *testing.T) {
	r := CompatCheck(`{"success":true,"msg":"ok"}`, DefaultResponseMarks())
	if r.Verdict != "success" {
		t.Fatalf("success=true 应判成功，实际 %+v", r)
	}
	if r.Rule == "" || r.Transient {
		t.Errorf("成功结论应给规则且非瞬态，实际 %+v", r)
	}
}

func TestCompatCheckRejectWithReason(t *testing.T) {
	r := CompatCheck(`{"success":false,"msg":"密码错误"}`, DefaultResponseMarks())
	if r.Verdict != "reject" {
		t.Fatalf("密码错误应判拒绝，实际 %+v", r)
	}
	if !strings.Contains(r.Detail, "密码错误") {
		t.Errorf("细节应带门户原因，实际 %+v", r)
	}
	if r.Transient {
		t.Errorf("硬拒绝不应标瞬态，实际 %+v", r)
	}
}

func TestCompatCheckTransientAndCustomMarks(t *testing.T) {
	r := CompatCheck("请求异常: timeout", DefaultResponseMarks())
	if r.Verdict != "reject" || !r.Transient {
		t.Errorf("默认瞬态词应标可重试，实际 %+v", r)
	}
	// 异校词表覆盖：自定义瞬态词应生效（同一判定链，不另抄一份）。
	m := DefaultResponseMarks()
	m.Transient = SplitMarks("busy")
	r2 := CompatCheck("portal busy", m)
	if !r2.Transient {
		t.Errorf("自定义 transient_marks=busy 应生效，实际 %+v", r2)
	}
}

func TestCompatCheckEmptyAndUndecidable(t *testing.T) {
	if r := CompatCheck("  ", DefaultResponseMarks()); r.Verdict != "reject" || r.Rule != "空响应" {
		t.Errorf("空响应结论应固定，实际 %+v", r)
	}
	long := strings.Repeat("<div>页面</div>", 40)
	if r := CompatCheck(long, DefaultResponseMarks()); r.Verdict != "reject" || !r.Transient {
		t.Errorf("长响应无法判定应归瞬态（不施加重罚），实际 %+v", r)
	}
}

func TestProtocolFingerprintACPortal(t *testing.T) {
	r := ProtocolFingerprint(`<form action="/ac_portal/login.php">opr=pwdLogin</form>`)
	if r.Family != "ac_portal" || r.Verdict != "compatible" {
		t.Fatalf("朴素 ac_portal 应判 compatible，实际 %+v", r)
	}
	if r.NeedsAuthTag || r.PwdEncrypted || r.Advice == "" {
		t.Errorf("朴素形态不该有令牌/加密标记，且要给建议，实际 %+v", r)
	}
}

func TestProtocolFingerprintVariantUnsupported(t *testing.T) {
	for _, sample := range []string{
		`/ac_portal/login.php auth_tag=xxx`,
		`/ac_portal/login.php password md5(`,
	} {
		r := ProtocolFingerprint(sample)
		if r.Family != "ac_portal" || r.Verdict != "variant_unsupported" {
			t.Errorf("%q 应判变体不支持，实际 %+v", sample, r)
		}
		if !r.NeedsAuthTag && !r.PwdEncrypted {
			t.Errorf("变体必须说明差异来源，实际 %+v", r)
		}
	}
}

func TestProtocolFingerprintOtherAndUnknown(t *testing.T) {
	cases := []struct {
		sample string
		family string
	}{
		{`/eportal/InterFace.do?method=login`, "eportal"},
		{`/cgi-bin/get_challenge?callback=jsonp`, "srun"},
		{`<html><body>hello</body></html>`, "unknown"},
	}
	for _, tc := range cases {
		r := ProtocolFingerprint(tc.sample)
		if r.Family != tc.family {
			t.Errorf("%q 应归 %s，实际 %+v", tc.sample, tc.family, r)
		}
		if r.Verdict == "compatible" || r.Advice == "" {
			t.Errorf("非朴素形态不得报 compatible 且必须给建议，实际 %+v", r)
		}
	}
}
