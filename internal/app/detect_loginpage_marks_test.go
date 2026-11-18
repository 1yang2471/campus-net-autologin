package app

import (
	"strings"
	"testing"
)

// 登录页指纹必须可配（2026-11-11 真机实测的反面教材）：
//
// 默认指纹是 ac_portal/pwdLogin，而本校登录页（/ac_portal/default/pc.html?tabs=pwd，
// 13813 B）正文里这两串**各出现 0 次**——表单由 JS 渲染。于是 `cnal detect` 与
// 首跑向导在本校恒失败，用户只能手填门户地址。稳定存在的标记是
// `<title>上网认证系统</title>` 与 `password_name`（表单字段 id）。
//
// 异校适配的正解是 ini 的 login_page_marks，不是把某校字符串写进代码。

// 本校登录页的真实形态（取自 2026-11-11 抓取的正文片段，逐字）。
const campusLoginPageBody = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html xmlns="http://www.w3.org/1999/xhtml"><head>
<meta id="viewport" name="viewport" content="width=device-width; initial-scale=0.9;">
<title>上网认证系统</title>
<script language="javascript" src="../share/res/js/pcauth.js"></script>
<script language="javascript" src="../share/res/js/logic_new.js"></script>
</head><body>
<div id="mode_login"><input id="password_name" type="text"></div>
</body></html>`

// 行为：默认指纹在本校页面上判负——这正是首跑探测失败的原因（钉住现状，防止
// 有人"顺手"把某校字符串加进默认值：默认值必须是通用族指纹）。
func TestDefaultMarksDoNotMatchCampusLoginPage(t *testing.T) {
	if portalBodyMatchesMarks(campusLoginPageBody, defaultLoginPageMarks()) {
		t.Fatal("默认指纹（ac_portal/pwdLogin）不应命中本校登录页——命中说明默认值被塞了某校私有标记")
	}
}

// 行为：配置了本校标记后判正（异校适配路径）。
func TestConfiguredMarksMatchCampusLoginPage(t *testing.T) {
	for _, marks := range [][]string{
		{"上网认证系统"},
		{"password_name"},
		{"ac_portal", "上网认证系统"}, // 与默认合并也应当命中
	} {
		if !portalBodyMatchesMarks(campusLoginPageBody, marks) {
			t.Errorf("指纹 %v 应命中本校登录页", marks)
		}
	}
}

// 行为：经典 ac_portal 页面在默认指纹下判正（异校零回归——默认档不能失效）。
func TestDefaultMarksStillMatchClassicAcPortalPage(t *testing.T) {
	classic := `<html><body><form action="/ac_portal/login.php">` +
		`<input name="userName"><input name="pwd" type="password"></form></body></html>`
	if !portalBodyMatchesMarks(classic, defaultLoginPageMarks()) {
		t.Error("经典 ac_portal 页面必须被默认指纹命中（默认档回归）")
	}
}

// 行为：空白项/大小写不应影响判定（配置里手写多一个逗号不该判负）。
func TestMarksIgnoreBlankAndCase(t *testing.T) {
	if !portalBodyMatchesMarks(campusLoginPageBody, []string{"  ", "上网认证系统", ""}) {
		t.Error("空白指纹项应被忽略，其余项仍生效")
	}
	if !portalBodyMatchesMarks("...PWDLOGIN...", []string{"pwdlogin"}) {
		t.Error("指纹匹配应大小写不敏感")
	}
	if portalBodyMatchesMarks(campusLoginPageBody, []string{"  ", ""}) {
		t.Error("全是空白项时不得判正")
	}
}

// 行为：ini 的 login_page_marks 要能读进来（逗号分隔，复用词表解析）。
func TestConfigParsesLoginPageMarks(t *testing.T) {
	ini := "[keeper]\nserver = https://x\nlogin_path = /a\n" +
		"login_page_marks = 上网认证系统, password_name\n"
	c, err := loadDaemonConfig(writeTmpIni(t, ini))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(c.LoginPageMarks, ",")
	if got != "上网认证系统,password_name" {
		t.Errorf("login_page_marks 解析错误，实际 %q", got)
	}
	// 未配置时必须是空（= 走默认档），不得被默认值污染成"已配置"。
	c2, err := loadDaemonConfig(writeTmpIni(t, "[keeper]\nserver = https://x\nlogin_path = /a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c2.LoginPageMarks) != 0 {
		t.Errorf("未配置 login_page_marks 时应为空，实际 %v", c2.LoginPageMarks)
	}
}

// 行为：探测必须**先不带 Host**、再带配置的 Host（2026-11-11 真机实证：本校把
// connect.rom.miui.com 放行了，带它的 Host 在未认证时也拿到真实 404，探测不到
// 劫持；不带 Host 才拿到 302→门户）。同一未认证态下的三组对照见探测函数注释。
func TestProbeHostVariantsTriesWithoutHostFirst(t *testing.T) {
	got := probeHostVariants("connect.rom.miui.com")
	if len(got) != 2 || got[0] != "" || got[1] != "connect.rom.miui.com" {
		t.Fatalf("应先试空 Host 再试配置值，实际 %#v", got)
	}
	if g := probeHostVariants(""); len(g) != 1 || g[0] != "" {
		t.Errorf("未配置 Host 时只试空 Host，实际 %#v", g)
	}
	if g := probeHostVariants("  "); len(g) != 1 || g[0] != "" {
		t.Errorf("空白 Host 应视同未配置，实际 %#v", g)
	}
}
