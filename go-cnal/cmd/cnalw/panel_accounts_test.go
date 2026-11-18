package main

import (
	"strings"
	"testing"
)

// 首屏「切账号」捷径：产品首屏是状态页，账号切换不能藏在设置里找不到。
// 捷径只做导航（跳设置页并聚焦历史账号选择器），不复制第二份账号列表——
// 列表唯一出处仍是 /accounts。

func TestPanelStatusHasAccountShortcut(t *testing.T) {
	page := panelPage()
	for _, want := range []string{
		`id="acct-shortcut"`,
		"切账号",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("状态页缺切账号入口: %s", want)
		}
	}
}

func TestAccountShortcutNavigatesToSettings(t *testing.T) {
	s := panelJS()
	for _, want := range []string{
		`"acct-shortcut"`,
		"showView(\"settings\")",
		`"f-account").focus()`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("切账号捷径缺导航/聚焦逻辑: %s", want)
		}
	}
}
