package main

import (
	"strings"
	"testing"
)

// 面板高级设置区（P1）：把 ini 已有字段收进可折叠区，沿用 DESIGN.md 的
// 现有组件（input/select/fieldset/card），不引入新色、新圆角或装饰动画。
// 测试描述 DOM 契约与 JS 行为，渲染验收由 release_verify 真浏览器负责。

func TestPanelPageHasAdvancedSection(t *testing.T) {
	page := panelPage()
	for _, want := range []string{
		`id="advanced"`,        // 折叠容器
		`id="adv-toggle"`,      // 折叠开关
		`id="f-interval"`,      // 数值键
		`id="f-success-marks"`, // [response] 词表
		`id="f-probe-timeout"`,
		`id="f-login-form"`, // 异校登录表单模板
		"advancedConfigView", // 读取/回填
		"collectAdvanced",    // 收集提交
	} {
		if !strings.Contains(page, want) {
			t.Errorf("面板页缺高级设置区内容: %s", want)
		}
	}
}

func TestPanelPageAdvancedUsesDesignSystem(t *testing.T) {
	css, err := panelFS.ReadFile("panel.css")
	if err != nil {
		t.Fatal(err)
	}
	s := string(css)
	// 折叠区只许用既有变量与控件类，不许新造颜色/阴影/动画。
	for _, want := range []string{".settings input", "var(--accent)", "details"} {
		if !strings.Contains(s, want) {
			t.Errorf("CSS 缺既有系统成分: %s", want)
		}
	}
	for _, bad := range []string{"@keyframes", "box-shadow", "linear-gradient", "animation:"} {
		if strings.Contains(s, bad) {
			t.Errorf("高级区不得引入装饰效果: %s", bad)
		}
	}
}

func TestPanelPageSetupHidesAdvanced(t *testing.T) {
	js := panelJS()
	if !strings.Contains(js, "advanced") || !strings.Contains(js, "SETUP") {
		t.Error("首跑模式应隐藏高级区（端点未接线时不宜暴露）")
	}
}
