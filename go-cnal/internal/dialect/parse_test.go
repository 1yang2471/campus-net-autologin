package dialect

import "testing"

func TestParsePortalPseudoJSON(t *testing.T) {
	// 形态取自 2026-11-06 汕大实测：单引号 + 冒号后空格（非合法 JSON）
	raw := "{'success':true, 'ip':'2001:db8:1::19', 'isOnline':false}"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("应能解析门户方言，实际报错: %v", err)
	}
	if m["success"] != true {
		t.Errorf("success 应为 true，实际 %v", m["success"])
	}
	if m["ip"] != "2001:db8:1::19" {
		t.Errorf("ip 应原样保留，实际 %v", m["ip"])
	}
	if m["isOnline"] != false {
		t.Errorf("isOnline 应为 false，实际 %v", m["isOnline"])
	}
}

func TestParseLoginSuccessText(t *testing.T) {
	// 登录响应是 JSON 但含单引号键值混合（E4 实测 logon success 那条）
	m, err := Parse(`{'success':true, 'msg':'logon success','action':'logout'}`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if m["msg"] != "logon success" {
		t.Errorf("msg 应为 logon success，实际 %v", m["msg"])
	}
}

func TestParseGarbageIsNotErrorButFlagged(t *testing.T) {
	// 登录页 HTML 之类的非字典响应：不 panic、不编造成功，返回 ok=false
	m, err := Parse("<html><body>ac_portal</body></html>")
	if err != nil {
		t.Fatalf("不应报错（要能识别为不可解析）: %v", err)
	}
	if _, ok := m["success"]; ok {
		t.Errorf("HTML 不得被解析出 success 键")
	}
}

// 以下两条来自 2026-11-06 用 Python 等价实现跑真实形态的设计验证：
// 这两类形态若漏掉，Go 版会在异校/失败路径上假绿。

func TestParseNestedQuotesInMsg(t *testing.T) {
	// 现网日志实有形态：PHP 风格序列化里 msg 内层还带单引号
	m, err := Parse("{'success':false,'msg':'Task 'NOAUTH:*' not taken','errno':3}")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if m["success"] != false {
		t.Errorf("success 应为 false")
	}
	if m["msg"] != "Task 'NOAUTH:*' not taken" {
		t.Errorf("内嵌单引号必须原样保留，实际 %q", m["msg"])
	}
}

func TestParseCommaInsideQuotedValue(t *testing.T) {
	// 中文 msg 与带千分位的余额里都有逗号：引号内的逗号不得当键值分隔
	m, _ := Parse("{'success':false,'msg':'未成功,请重试','userBalance':'1,024'}")
	if m["msg"] != "未成功,请重试" {
		t.Errorf("逗号被误当分隔符: %q", m["msg"])
	}
	if m["userBalance"] != "1,024" {
		t.Errorf("余额值被拆坏: %q", m["userBalance"])
	}
}

func TestParseKeepsNumbersAsStrings(t *testing.T) {
	// 设计决定（不是缺陷）：数值一律留字符串。门户的 pop/errno 只用于展示与
	// 存在性判断，一旦引入数值强转就要处理 3 vs "3" 的两套调用点——留字符串
	// 让"判 success/isOnline"保持唯一职责。需要数值时由调用方显式 strconv。
	m, _ := Parse("{'success':true,'pop':0,'errno':'3'}")
	if v, ok := m["pop"].(string); !ok || v != "0" {
		t.Errorf("pop 应保持字符串形态，实际 %T %v", m["pop"], m["pop"])
	}
}
