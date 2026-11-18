package protocol

import (
	"strings"
	"testing"
)

// P2 审查：静态值里的 percent-encoding 会被 EncodeForm 二次编码
// （next=%2Findex → next=%252Findex）。模板值是原始字面量，从浏览器
// devtools 抄来的 encoded 形态必须拒绝并指引写原文，防静默发错数据。

func TestParseFormRejectsPercentInStaticValue(t *testing.T) {
	f, err := ParseForm("opr=login&userName={user}&pwd={pass}&next=%2Findex")
	if err == nil {
		t.Fatalf("静态值含 %% 必须拒绝（会被二次编码），实际 %v", f.Template())
	}
	if !strings.Contains(err.Error(), "%") {
		t.Errorf("错误信息应提到 %% 编码问题，实际 %v", err)
	}
}

func TestParseFormAllowsPercentInPlaceholdersOnly(t *testing.T) {
	// {user}/{pass} 占位本身不含 %；凭据里的 % 由 EncodeForm 统一编码，
	// 不属于本护栏范围。静态键含 % 同样拒绝（键不会进 URL 编码例外）。
	if _, err := ParseForm("opr=login&user%name={user}&pwd={pass}"); err == nil {
		t.Error("键含 % 也应拒绝")
	}
}

// 异校回放（**只读测量，不改行为**）：锐捷 eportal 的真实登录表里，queryString
// 这个字段的值**本身就是一整段查询串**（wlanuserip=…&wlanacname=…&ssid=…），
// 在门户自己的抓包里它是百分号编码形态（来源见 docs/corpus-异校响应语料.md）。
//
// 本测试钉住当前行为 = 「这类门户写不出真实配置」——两条路都被堵死：
//  1. 写编码形态（%3D/%26）→ 被上面的 %% 护栏拒绝；
//  2. 写未转义原文（含 &/=）→ 被模板语法本身切碎（内层 & 泄漏成顶层字段）。
//
// 这是**实测到的能力缺口**，不是期望行为。将来若给模板加上「某字段的值可承载
// 含 &/= 的原文、且只编码一次」的语法（引号、raw 前缀之类），本测试会变红，
// 提醒把这条从缺口清单里划掉。
func TestCorpusRuijieQueryStringFormIsNotExpressible(t *testing.T) {
	// 值里的 IP / MAC / AC 名一律已脱敏为占位符。
	const tail = "&operatorUserId=&operatorPwd=&validcode=&passwordEncrypt=false"

	encoded := "userId={user}&password={pass}&service=" +
		"&queryString=wlanuserip%3D1.2.3.4%26wlanacname%3DAC%26ssid%3DSSID" +
		"%26nasip%3D1.2.3.4%26mac%3DMAC%26t%3Dwireless-v2%26url%3D" + tail
	if _, err := ParseForm(encoded); err == nil {
		t.Error("编码形态现在被接受了？那就该把这条缺口从文档里划掉")
	}

	raw := "userId={user}&password={pass}&service=" +
		"&queryString=wlanuserip=1.2.3.4&wlanacname=AC&ssid=SSID" +
		"&nasip=1.2.3.4&mac=MAC&t=wireless-v2&url=" + tail
	f, err := ParseForm(raw)
	if err != nil {
		t.Fatalf("未转义原文现在也报错了（%v）——缺口形态变了，请更新本条与文档", err)
	}
	fields := f.Fields("u", "p")
	if fields["queryString"] != "wlanuserip=1.2.3.4" {
		t.Fatalf("预期内层 = 把值截成半段，实际 %q", fields["queryString"])
	}
	if _, leaked := fields["wlanacname"]; !leaked {
		t.Fatal("预期内层 & 把 wlanacname 泄漏成顶层字段——若已不泄漏，说明模板能表达了，请划掉这条缺口")
	}
	t.Logf("缺口复现：期望 8 个字段承载整段 queryString，实际被切成 %d 个字段", len(fields))
}
