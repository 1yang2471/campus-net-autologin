package keeper

import (
	"strings"
	"testing"
)

// 异校响应回放语料（**公开资料，不是异校实测**）。
//
// 目的：量出「现有可配置面（[response] 词表）到底能覆盖哪些学校的真实应答」，
// 而不是在参考校一台机器上宣称通用。每条样本都带来源 URL、逐字与否、脱敏说明
// 与可信度；凡是来源只给了判定逻辑、没给响应原文的，标 verbatim=false，不冒充
// 逐字引用。凭据/学号/私有域名/IP 一律已替换为占位符。
//
// 三分类（这就是覆盖度测量本身）：
//  1. 默认词表就够：marksFix == nil && unfixable == ""；
//  2. 需要词表覆盖：marksFix != nil —— 断言"默认必错"且"覆盖后必对"，
//     把「异校要写什么」变成可执行的事实；
//  3. 词表覆盖也表达不了：unfixable != "" —— 这才是真缺口（要改代码）。
//
// 已知的边界：本语料只回放**判定层**（JudgeLoginResponse）。端点形态、传输
// 方式（GET 型登录）、密码加密（RC4/SRBX1）都不在这里——那些是 login_form /
// 协议层的事，另有缺口。

type wantKind string

const (
	wantOK      wantKind = "ok"       // 来源自身判定为成功
	wantAlready wantKind = "already"  // 来源判为「已在线」（重复登录哨兵）
	wantReject  wantKind = "rejected" // 来源判为失败（凭据错/被拒）
)

// ourOutcome 我们这一侧的四态分类（由 JudgeLoginResponse 的结果反推）。
type ourOutcome string

const (
	outOK       ourOutcome = "ok"
	outAlready  ourOutcome = "already"
	outRejected ourOutcome = "rejected"
	outEmpty    ourOutcome = "empty"
)

type corpusEntry struct {
	id       string
	family   string
	source   string // 来源 URL
	verbatim bool   // body 是否逐字引自来源（false = 依来源判定字段补的形态）
	note     string // 脱敏说明 / 可信度 / 形态来源
	body     string
	want     wantKind
	// marksFix 非空 = 该校必须覆盖词表才成立（nil 表示默认词表应当就够）。
	marksFix *ResponseMarks
	// unfixable 非空 = 词表覆盖也表达不了（写明要改什么）。
	unfixable string
}

func corpus() []corpusEntry {
	return []corpusEntry{
		// —— ac_portal 族（本项目目标族）——
		{
			id: "acp-pseudo-ok", family: "ac_portal", verbatim: true,
			source: "https://github.com/Linvery/sangfor-ngac-autologon/blob/master/school_netlogin.py",
			note:   "深信服：来源的一手解析依据是「响应首 16 字符 message[1]=='true,'」，故原文即该前缀；非完整响应体",
			body:   `{'success':true,`,
			want:   wantOK,
		},
		{
			id: "acp-pseudo-fail", family: "ac_portal", verbatim: true,
			source: "https://github.com/Linvery/sangfor-ngac-autologon/blob/master/school_netlogin.py",
			note:   "同上，false 分支（来源：message[1]=='false' → 失败）",
			body:   `{'success':false,`,
			want:   wantReject,
		},
		{
			id: "acp-already-online", family: "ac_portal", verbatim: false,
			source: "https://github.com/victorwoo/Auth-Sinfor/blob/master/Auth-Sinfor.ps1",
			note:   "msg 逐字引自来源（判定用 $response.success + msg 比对）；success/msg 外壳按来源判定字段补齐，非原文",
			body:   `{'success':false,'msg':'用户已在线，不需要再次认证'}`,
			want:   wantAlready,
		},
		{
			id: "acp-text-logon-success", family: "ac_portal", verbatim: true,
			source: "https://git.mitsea.com/FlintyLemming/MitseaBlog（搜索快照，原页 404）",
			note:   "低可信：只从搜索摘要拿到「提示 logon success，成功」，未抽到原页",
			body:   `logon success`,
			want:   wantOK,
		},

		// —— Dr.COM / eportal 族 ——
		{
			id: "drcom-result0-ok", family: "Dr.COM", verbatim: false,
			source: "https://blog.ataw.top/archives/drcom-auto-login",
			note:   "result:0 为成功；原文字段的回调名被调研工具脱敏，此处用占位名 dr1003（不冒充原文）",
			body:   `dr1003([{result: 0, ret_code: 0, msg: '登录成功'}])`,
			want:   wantOK,
		},
		{
			id: "drcom-result1-escaped-ok", family: "Dr.COM", verbatim: true,
			source: "https://www.jb51.net/article/230647.htm",
			note:   "result:\"1\" 为成功；msg 是 \\u 转义中文（原文如此）",
			body:   `dr1004({"result":"1","msg":"\u8ba4\u8bc1\u6210\u529f"})`,
			want:   wantOK,
			marksFix: &ResponseMarks{
				// 转义后正文里没有「成功」二字，只能按结构键认成功。
				Success: SplitMarks(`成功,登录成功,login_ok,"result":"1"`),
			},
		},
		{
			id: "drcom-result1-loose-ok", family: "Dr.COM", verbatim: true,
			source: "https://www.jb51.net/article/230647.htm",
			note:   "同源另一写法（键无引号，msg 未转义）",
			body:   `dr1004({result: "1", msg: "认证成功"})`,
			want:   wantOK,
		},

		// —— 锐捷 eportal 族 ——
		{
			id: "rj-success", family: "锐捷 eportal", verbatim: false,
			source: "https://github.com/GuoYingLong/eportal-login/blob/master/login-eportal.py",
			note:   "来源判定依据是 result 含 'success'；成功体的 message 未见逐字，故标非逐字",
			body:   `{"result":"success","message":"认证成功"}`,
			want:   wantOK,
		},
		{
			id: "rj-bad-password", family: "锐捷 eportal", verbatim: true,
			source: "https://github.com/GuoYingLong/eportal-login/blob/master/login-eportal.py",
			note:   "message 逐字引自来源",
			body:   `{"result":"fail","message":"密码不匹配,请输入正确的密码!"}`,
			want:   wantReject,
		},
		{
			id: "rj-no-user", family: "锐捷 eportal", verbatim: true,
			source: "https://github.com/GuoYingLong/eportal-login/blob/master/login-eportal.py",
			note:   "message 逐字引自来源",
			body:   `{"result":"fail","message":"用户不存在"}`,
			want:   wantReject,
		},

		// —— 深澜 Srun 族 ——
		{
			id: "srun-ok", family: "Srun", verbatim: true,
			source: "https://blog.sleepstars.net/archives/shen-da-xiao-yuan-wang-linux-zi-dong-ren-zheng-srun-xie-yi-jie-xi-yu-python-shi-xian",
			note:   "access_token 已脱敏为 TOKEN",
			body:   `{"error":"ok","ecode":0,"suc_msg":"login_ok","ploy_msg":"E0000: Login is successful.","access_token":"TOKEN"}`,
			want:   wantOK,
			// 字段名就叫 error，而默认负向词表里有裸 error —— 成功体被判失败。
			// 覆盖词表可救（同时必须收窄负向词，否则 error 仍先命中）。
			marksFix: &ResponseMarks{
				Success:  SplitMarks("成功,登录成功,login_ok"),
				Negative: SplitMarks("未成功,不成功,失败,错误,not_online_error,no_response_data_error"),
			},
		},
		{
			id: "srun-already-online", family: "Srun", verbatim: true,
			source: "https://blog.sleepstars.net/archives/shen-da-xiao-yuan-wang-linux-zi-dong-ren-zheng-srun-xie-yi-jie-xi-yu-python-shi-xian",
			note:   "已在线语义；suc_msg 里带 error 字样",
			body:   `{"error":"ok","suc_msg":"ip_already_online_error"}`,
			want:   wantAlready,
			unfixable: "「已在线」哨兵是硬编码中文正则（sessfalse.go 的 reAlreadyOnline），" +
				"词表只能表达正/负向，表达不了「这是已在线」。要支持英文族需给 already-online 单独开配置项。",
		},
		{
			id: "srun-not-online", family: "Srun", verbatim: true,
			source: "https://huhu.ciduid.top/2022/1113/srun-login-analyze/",
			note:   "未在线语义（判为失败）",
			body:   `{"error":"ok","suc_msg":"not_online_error"}`,
			want:   wantReject,
		},

		// —— 江苏科技大学校方公布的失败文案（一手）——
		{
			id: "just-wrong-password", family: "其他（校方文案）", verbatim: true,
			source: "http://ic.just.edu.cn/2023/0412/c6175a322383/page.htm",
			note:   "校方自助页公布的失败文案，逐字",
			body:   `密码不正确！ password wrong!`,
			want:   wantReject,
		},
		{
			id: "just-user-locked", family: "其他（校方文案）", verbatim: true,
			source: "http://ic.just.edu.cn/2023/0412/c6175a322383/page.htm",
			note:   "同上",
			body:   `User locked`,
			want:   wantReject,
		},
		{
			id: "just-no-account", family: "其他（校方文案）", verbatim: true,
			source: "http://ic.just.edu.cn/2023/0412/c6175a322383/page.htm",
			note:   "同上",
			body:   `在系统内未查询到该账号！请先开户！`,
			want:   wantReject,
		},
	}
}

// classify 把我们这一侧的结果归成四态（错误文本里区分「无法判定」= 瞬态）。
func classify(resp string) ourOutcome {
	out, err := JudgeLoginResponse(resp, ResponseMarks{})
	if err == nil {
		if alreadyOnline(out) {
			return outAlready
		}
		return outOK
	}
	if strings.Contains(err.Error(), "空响应") {
		return outEmpty
	}
	return outRejected
}

// 分类 1：默认词表就够的学校——这是「开箱可用」的实测边界。
func TestCorpusReplayDefaultMarksSufficient(t *testing.T) {
	n := 0
	for _, e := range corpus() {
		if e.marksFix != nil || e.unfixable != "" {
			continue
		}
		n++
		if got := classify(e.body); string(got) != string(e.want) {
			t.Errorf("%s（%s，来源 %s）：默认词表下判成 %s，来源自身判定是 %s\n  正文: %s",
				e.id, e.family, e.source, got, e.want, e.body)
		}
	}
	if n == 0 {
		t.Fatal("语料里没有「默认就够」的样本，分类写错了")
	}
	t.Logf("默认词表即可覆盖 %d 条样本", n)
}

// 分类 2：需要覆盖词表的学校——同时断言「默认必错」与「覆盖后必对」，
// 让「异校要在 [response] 里写什么」成为可执行的事实，而不是文档里的建议。
func TestCorpusReplayMarksOverrideNecessaryAndSufficient(t *testing.T) {
	for _, e := range corpus() {
		if e.marksFix == nil {
			continue
		}
		if got := classify(e.body); string(got) == string(e.want) {
			t.Errorf("%s：默认词表已经能判成 %s，就不该列进「需要覆盖词表」——"+
				"请移到默认覆盖组，否则这条覆盖建议是假的", e.id, e.want)
		}
		out, err := JudgeLoginResponse(e.body, *e.marksFix)
		got := outOK
		if err != nil {
			got = outRejected
		} else if alreadyOnline(out) {
			got = outAlready
		}
		if string(got) != string(e.want) {
			t.Errorf("%s（%s）：按给定词表覆盖后仍判成 %s，期望 %s\n  覆盖: %+v\n  正文: %s",
				e.id, e.family, got, e.want, *e.marksFix, e.body)
		}
	}
}

// 分类 3：词表覆盖也表达不了的——真缺口。这里断言「默认确实判错」，
// 缺口一旦被修（判对了）本测试会红，提醒把它移出 unfixable。
func TestCorpusReplayUnfixableGapsAreReal(t *testing.T) {
	n := 0
	for _, e := range corpus() {
		if e.unfixable == "" {
			continue
		}
		n++
		if got := classify(e.body); string(got) == string(e.want) {
			t.Errorf("%s：现在已能判成 %s，缺口不存在了——请移出 unfixable 并更新文档", e.id, e.want)
		}
	}
	if n == 0 {
		t.Log("没有「词表表达不了」的样本（与调研结论不符时应复核）")
	}
}

// 语料自身的完整性：id 不重复、来源非空、want 合法——防止语料腐化。
func TestCorpusIntegrity(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range corpus() {
		if e.id == "" || e.source == "" || e.body == "" {
			t.Errorf("语料项字段缺失: %+v", e)
		}
		if seen[e.id] {
			t.Errorf("id 重复: %s", e.id)
		}
		seen[e.id] = true
		switch e.want {
		case wantOK, wantAlready, wantReject:
		default:
			t.Errorf("%s: want 非法 %q", e.id, e.want)
		}
		if e.marksFix != nil && e.unfixable != "" {
			t.Errorf("%s: 不能同时是「可覆盖」和「不可覆盖」", e.id)
		}
	}
}
