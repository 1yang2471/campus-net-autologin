package keeper

import (
	"testing"

	"cnal/internal/protocol"
)

// 通用化可回放判据（Step 5）：复用 corpus_replay_test.go 里的真实公开语料
// （corpus()），把「异校通用化」拆成四个可执行事实 + 一条反例。
//
// 语料正文/来源/verbatim 一律沿用 corpus()，本文件不复制、不编造新样本。
// 来源行（docs/corpus-异校响应语料.md）：
//   - §2 来源清单（第 34-49 行）：每条 id 的来源与 verbatim 口径；
//   - §3.1 需要写 [response] 的两所（第 55-74 行）：srun-ok / drcom 转义成功；
//   - §3.2 词表表达不了的一所（第 76-81 行）：srun 已在线哨兵；
//   - §4 本层回放不了的三类（第 107-113 行）：GET 端点/密码加密/探测指纹。
//
// 围栏（守诚实边界）：这里只回放「判定层 + 表单解析层」。GET 端点能否真投递、
// 密码加密、探测指纹都不在本层（文档 §4），不在本文件假绿——那三类的缺口仍原样
// 留在文档里，等真机或适配层验证，不以「测试全绿」冒充通用化已达成。

// corpusByID 从 corpus() 取样本，避免在本文件里再抄一份正文（真相源单一）。
func corpusByID(t *testing.T, id string) corpusEntry {
	t.Helper()
	for _, e := range corpus() {
		if e.id == id {
			return e
		}
	}
	t.Fatalf("语料 %q 不在 corpus() 里（语料被改过？）", id)
	return corpusEntry{}
}

// (a) GET 型登录表单经 ParseForm 得 Method=GET 且字段正确。
// 独立于 detect（agent 2）与投递层：只断言解析结果本身，不依赖任何探测/网络。
// 模板形状取参考校默认字段集（protocol.DefaultFormTemplate）加 GET 前缀——
// 这是解析层探针，不冒充某异校真实 login_form（真实字段名是校事实，见文档 §4）。
func TestGenericReplayGetFormParse(t *testing.T) {
	const (
		user = "stu001"
		pass = "pw123"
	)
	cases := []struct {
		name       string
		tmpl       string
		wantMethod string
		wantFields map[string]string
	}{
		{
			name:       "GET 前缀进 Method 不进字段",
			tmpl:       "GET&opr=pwdLogin&userName={user}&pwd={pass}&rememberPwd=1",
			wantMethod: "GET",
			wantFields: map[string]string{
				"opr": "pwdLogin", "userName": user, "pwd": pass, "rememberPwd": "1",
			},
		},
		{
			name:       "无前缀默认 POST（参考校零改动）",
			tmpl:       "opr=pwdLogin&userName={user}&pwd={pass}&rememberPwd=1",
			wantMethod: "POST",
			wantFields: map[string]string{
				"opr": "pwdLogin", "userName": user, "pwd": pass, "rememberPwd": "1",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := protocol.ParseForm(c.tmpl)
			if err != nil {
				t.Fatalf("解析 %q 失败: %v", c.tmpl, err)
			}
			if f.Method != c.wantMethod {
				t.Errorf("Method 应为 %q，实际 %q", c.wantMethod, f.Method)
			}
			got := f.Fields(user, pass)
			if len(got) != len(c.wantFields) {
				t.Errorf("字段数应为 %d（方法前缀不得进表单），实际 %v", len(c.wantFields), got)
			}
			for k, v := range c.wantFields {
				if got[k] != v {
					t.Errorf("字段 %q 应为 %q，实际 %q", k, v, got[k])
				}
			}
			if _, leaked := got["GET"]; leaked {
				t.Error("GET 方法前缀漏进了表单字段")
			}
		})
	}
}

// (b) 默认词表能判成功的真实应答回放。
// 样本来自文档 §2 来源清单：acp-pseudo-ok（第 34 行）、drcom-result0-ok（第 38 行）、
// rj-success（第 41 行）——都是从 corpus() 原样取的公开语料，非新编。
func TestGenericReplayDefaultMarksSuccess(t *testing.T) {
	for _, id := range []string{"acp-pseudo-ok", "drcom-result0-ok", "rj-success"} {
		t.Run(id, func(t *testing.T) {
			e := corpusByID(t, id)
			if e.marksFix != nil || e.unfixable != "" || e.want != wantOK {
				t.Fatalf("%s 不是「默认就够且判成功」的样本，选错例了", id)
			}
			if got := classify(e.body); got != outOK {
				t.Errorf("%s（来源 %s）：默认词表下判成 %s，应判成功。正文: %s",
					id, e.source, got, e.body)
			}
		})
	}
}

// (c) 写词表可救的真实应答回放：断言「默认必错」且「覆盖后必对」。
// 样本 srun-ok（文档 §3.1 第 57-66 行）、drcom-result1-escaped-ok（第 68-74 行）。
// 这也把「异校要在 [response] 里写什么」变成可执行事实，而非文档建议。
func TestGenericReplayMarksOverrideRescues(t *testing.T) {
	for _, id := range []string{"srun-ok", "drcom-result1-escaped-ok"} {
		t.Run(id, func(t *testing.T) {
			e := corpusByID(t, id)
			if e.marksFix == nil {
				t.Fatalf("%s 不是「写词表可救」的样本，选错例了", id)
			}
			if got := classify(e.body); string(got) == string(e.want) {
				t.Errorf("%s：默认词表已判成 %s，就不该列进「需要覆盖词表」", id, e.want)
			}
			out, err := JudgeLoginResponse(e.body, *e.marksFix)
			if err != nil {
				t.Errorf("%s：按给定词表覆盖后仍失败（%v），应判成功。覆盖: %+v；正文: %s",
					id, err, *e.marksFix, e.body)
				return
			}
			if alreadyOnline(out) {
				t.Errorf("%s：覆盖后判成「已在线」，期望成功", id)
			}
		})
	}
}

// (d) 表达不了的语料明确判失败——守住诚实边界，别假绿。
// 样本 srun-already-online（文档 §3.2 第 76-81 行）：「已在线」哨兵 ip_already_online_error
// 是英文，我们的哨兵是硬编码中文正则（sessfalse.go 的 reAlreadyOnline），词表表达不了。
// 这里断言默认词表把它判成失败（outRejected），而不是假装我们读得懂；一旦哪天修好了
// （判成 already），本测试会红，提醒把它从 unfixable 移出并更新文档。
func TestGenericReplayUnfixableStaysFailure(t *testing.T) {
	e := corpusByID(t, "srun-already-online")
	if e.unfixable == "" {
		t.Fatal("srun-already-online 不再是「表达不了」的样本，选错例了")
	}
	if got := classify(e.body); got != outRejected {
		t.Errorf("%s（来源 %s）：诚实边界应判失败(outRejected)，实际 %s。正文: %s",
			e.id, e.source, got, e.body)
	}
	// 双保险：即便不靠 classify，直接看默认判定也不能被当成「已在线」成功哨兵。
	_, err := JudgeLoginResponse(e.body, ResponseMarks{})
	if err == nil {
		t.Error("「已在线」英文哨兵不该被默认判定悄悄判成成功（那才是假绿）")
	}
}

// 反例（变异检查思路）：证明 (c)/(b) 不是 tautology——词表写错时测试确实会失败。
// 三条反例均来自文档 §3.1 的原文结论，不是凭空构造：
//  1. srun-ok 只加 success 词、却不收窄默认负向词 → 裸 error 仍先命中 → 仍失败
//     （文档第 59-60 行：「且必须同时收窄负向词，否则 error 仍先命中，只加 success 词无效」）；
//  2. drcom 转义成功体只给默认 success 词、缺结构键 "result":"1" → 正文没有「成功」二字 → 仍失败
//     （文档第 68-70 行：「正文里没有成功二字，只能按结构键认成功」）；
//  3. 把 srun-ok 的默认负向词硬塞回去 → 即使 success 对了也仍失败。
func TestGenericReplayWrongMarksStillFails(t *testing.T) {
	t.Run("srun 只加正向词不收窄负向词仍失败", func(t *testing.T) {
		e := corpusByID(t, "srun-ok")
		// Success 与正确覆盖一致，但 Negative 留空 → withDefaults 回落含裸 error 的默认负向词。
		wrong := ResponseMarks{Success: SplitMarks("成功,登录成功,login_ok")}
		if out, err := JudgeLoginResponse(e.body, wrong); err == nil {
			t.Errorf("错误词表（未收窄裸 error）下不该判成功，实际成功: %q", out)
		}
	})

	t.Run("drcom 转义体缺结构键仍失败", func(t *testing.T) {
		e := corpusByID(t, "drcom-result1-escaped-ok")
		// 只给默认 success 词，不给 "result":"1" 这个结构键——转义后的正文里没有「成功」。
		wrong := ResponseMarks{Success: SplitMarks("成功,登录成功,login_ok")}
		if out, err := JudgeLoginResponse(e.body, wrong); err == nil {
			t.Errorf("错误词表（缺结构键）下不该判成功，实际成功: %q", out)
		}
	})

	t.Run("srun 默认负向词塞回仍失败", func(t *testing.T) {
		e := corpusByID(t, "srun-ok")
		// success 收对了，但负向词里保留裸 error。
		wrong := ResponseMarks{
			Success:  SplitMarks("成功,登录成功,login_ok"),
			Negative: SplitMarks("未成功,不成功,失败,错误,error,fail"),
		}
		if out, err := JudgeLoginResponse(e.body, wrong); err == nil {
			t.Errorf("错误词表（负向词含裸 error）下不该判成功，实际成功: %q", out)
		}
	})
}

// 注：三条反例自带自证——若哪天把错误词表误改成正确覆盖，上面的
// 「不该判成功」断言立刻变红，不会留下空断言；无需再叠一层自检。
