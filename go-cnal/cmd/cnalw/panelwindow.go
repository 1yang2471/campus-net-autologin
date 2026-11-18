package main

import (
	"fmt"
	"runtime"

	"github.com/jchv/go-webview2"
)

// openPanelWindow 原生窗口打开面板（WebView2 壳，系统自带运行时——不引浏览器
// 进程树、不要求用户找地址）。窗口尺寸固定；状态视图单屏，设置视图内容较长
// 可滚动（2026-11-08 分栏后实测：状态页 < 760px，设置页仍超）。
// 失败回落 openURL（浏览器），两种路径同一份 panel.html。WebView2 的消息泵
// 阻塞到窗口关闭，因此必须在独立 goroutine 里起（托盘线程不能被占死）。
//
// 线程纪律（2026-11-07 实证：goroutine 未锁线程 → 窗口消息泵被调度器搬走
// → 面板未响应只能杀进程）：窗口创建与消息泵必须钉死在同一 OS 线程，
// LockOSThread 必须在 NewWithOptions 之前。IconId 取 exe 资源里的 app.ico
// （rsrc 默认资源 ID 1）——否则标题栏显示默认图标。
func openPanelWindow(url string) error {
	errCh := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		w := webview2.NewWithOptions(webview2.WebViewOptions{
			WindowOptions: webview2.WindowOptions{
				Title:  "校园网守护",
				Width:  420,
				Height: 760,
				IconId: 1,
			},
		})
		if w == nil {
			errCh <- fmt.Errorf("WebView2 初始化失败（运行时缺失？）")
			return
		}
		// 尺寸按状态视图定：状态卡+动作+诊断+日志入口实测约 500px；设置视图
		// 内容更长，允许滚动（2026-11-08 分栏后重新实测）。
		w.SetSize(420, 760, webview2.HintFixed)
		w.Navigate(url)
		w.Run() // 阻塞到窗口关闭
		w.Destroy()
		close(errCh)
	}()
	return <-errCh
}
