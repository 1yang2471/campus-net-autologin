package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 面板页唯一出处是 panelPage()（HTML/CSS/JS 四份源料拼回）。两个消费点：
//   - 浏览器验收脚本读 docs/screenshots/panel-a11y.html；
//   - 安卓 M1 壳把它作为 assets/panel.html 打进 APK，供 mobile.SetPanelPage。
// 由同一个函数导出，避免脚本/Kotlin 各自复刻一次拼接逻辑。
func TestExportPanelPageForBrowser(t *testing.T) {
	page := []byte(panelPage())
	targets := []string{
		filepath.Join("..", "..", "docs", "screenshots", "panel-a11y.html"),
		filepath.Join("..", "..", "..", "go-cnal-android", "app", "src", "main", "assets", "panel.html"),
	}
	for _, out := range targets {
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, page, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
