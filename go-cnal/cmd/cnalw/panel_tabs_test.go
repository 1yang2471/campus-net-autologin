package main

import (
	"strings"
	"testing"
)

// 面板状态/设置分栏：状态页默认可见、5s 轮询；设置页首次打开才读配置/账号。
// 最小化/切到设置页暂停轮询（省 CPU 与唤醒）。DOM 契约由 release_verify
// 真浏览器最终验收，这里守住结构不被回退。

func TestPanelPageHasTabs(t *testing.T) {
	page := panelPage()
	for _, want := range []string{
		`id="tabs"`, `role="tablist"`,
		`id="tab-status"`, `id="tab-settings"`,
		`id="view-status"`, `id="view-settings"`,
		`role="tabpanel"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("面板缺分栏结构: %s", want)
		}
	}
}

func TestPanelSettingsLazyLoaded(t *testing.T) {
	s := panelJS()
	for _, want := range []string{
		"function showView",
		`name === "settings" && !configLoaded`,
		"visibilitychange",
		"function syncPolling",
		"clearInterval",
		"ArrowRight", // tablist 键盘模式（左右箭头切换）
	} {
		if !strings.Contains(s, want) {
			t.Errorf("面板缺懒加载/暂停轮询逻辑: %s", want)
		}
	}
	// 顶层无条件 loadConfig() 会让状态页也打 /config 与 /accounts（懒加载失效）。
	if strings.Contains(s, "\nloadConfig();") {
		t.Error("loadConfig 不得在顶层无条件调用（应只在设置页打开/首跑时）")
	}
}

// P1 审查：配置没读到时禁止整单保存——空 server/auto_reconnect=false 原样
// 提交会毁掉运行配置（自动重连被静默关闭、重启掉回首跑面板）。
func TestPanelSaveGuardsUnloadedConfig(t *testing.T) {
	s := panelJS()
	for _, want := range []string{
		"!configLoaded && !SETUP", // 保存闸：未读取且非首跑 → 拒绝
		"配置未读取",                   // 失败必须可见并给下一步
		"body.login_form",         // login_form 保存后提示重启生效
		"登录表单重启后生效",               // 文案不得宣称热生效
	} {
		if !strings.Contains(s, want) {
			t.Errorf("面板缺保存护栏/生效提示: %s", want)
		}
	}
	// SETUP 例外：首跑本来就从空开始，不得被闸挡住。
	if !strings.Contains(s, "!configLoaded && !SETUP") {
		t.Error("保存闸必须豁免 SETUP（首跑从空配置开始）")
	}
}

// P2 审查：首跑保留状态卡（setup.go 的引导文案是用户唯一可见说明），
// 但藏掉运行期卡片；非首跑走 showView 正常分栏。
func TestPanelSetupKeepsStatusCard(t *testing.T) {
	s := panelJS()
	for _, want := range []string{
		`document.getElementById("actions-card").hidden = true`,
		`document.getElementById("diag-card").hidden = true`,
		"VIEWS.status.panel.hidden = false", // 状态卡（引导文案）保留可见
		"VIEWS.settings.panel.hidden = false",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("SETUP 应保留状态卡并藏运行期卡: %s", want)
		}
	}
}

func TestPanelHasLoginFormTemplateField(t *testing.T) {
	page := panelPage()
	for _, want := range []string{
		`id="login-form-adv"`, `id="f-login-form"`, "login_form",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("面板缺登录表单模板字段: %s", want)
		}
	}
}

func TestPanelCSSHasTouchTargets(t *testing.T) {
	css, err := panelFS.ReadFile("panel.css")
	if err != nil {
		t.Fatal(err)
	}
	s := string(css)
	if !strings.Contains(s, "@media (pointer: coarse)") || !strings.Contains(s, "min-height: 44px") {
		t.Error("触屏可点面应 ≥44px（Android M1 门槛）")
	}
	// details/summary 是原生折叠开关，默认 min-height:0；漏进 coarse 规则
	// 等于高级设置在手机上点不开（真浏览器实测抓到过）。只要求它出现在
	// coarse 块里且该块给出 44px——不钉死整行格式，换写法不该让测试变红。
	i := strings.Index(s, "@media (pointer: coarse)")
	if i < 0 || !strings.Contains(s[i:], "details summary") ||
		!strings.Contains(s[i:], "min-height: 44px") {
		t.Error("coarse 规则必须覆盖 details summary 且给出 ≥44px")
	}
	// 禁用按钮 hover 不得点亮边框：绝对标准的 disabled 态行为。
	if !strings.Contains(s, "button:hover:not(:disabled)") {
		t.Error("hover 必须排除 :disabled，否则禁用按钮给出可点暗示")
	}
	// 分栏沿用既有调色板：不得引入新颜色/阴影/动画。
	for _, bad := range []string{"box-shadow", "@keyframes", "animation:", "linear-gradient"} {
		if strings.Contains(s, bad) {
			t.Errorf("分栏不得引入装饰效果: %s", bad)
		}
	}
}
