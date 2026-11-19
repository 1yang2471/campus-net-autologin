package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"cnal/internal/app"
	"cnal/internal/keeper"
	"github.com/getlantern/systray"
)

// cfgPathFlag 托盘动作要用的配置路径（main 里赋值一次）。
var cfgPathFlag string

// cnalw 托盘壳：内核 + 回路 API + 托盘图标/菜单（Windows 用户面）。
// 组合根在 internal/app，与 cnald 无头壳共吃一份接线。
func main() {
	fs := flag.NewFlagSet("cnalw", flag.ExitOnError)
	cfgPath := fs.String("config", "", "守护同款 ini 路径（[keeper] 节）")
	printURL := fs.Bool("print-panel-url", false, "打印面板 URL 后继续运行（URL 含 token，只贴给自己）")
	openPanel := fs.Bool("open-panel", false, "启动后直接打开原生面板窗口")
	fs.Parse(os.Args[1:])
	if *cfgPath == "" {
		// 缺省即 Python 守护同款 config.ini：新机双击 cnalw 不用记路径。
		*cfgPath = app.ConfigFilePath()
	}
	cfgPathFlag = *cfgPath
	// 单实例互斥先于一切（Compose 会读凭据/触网）：两个守护并行会互抢登录、
	// 撞门户反暴力冻结（2026-11-08 用户实证）。锁是 OS 级字节锁，进程死了
	// 自动释放，不存在残留锁阻塞重启。
	if release, ok := app.AcquireInstanceLock(); !ok {
		showMessage("校园网守护已在运行（托盘图标在任务栏通知区）。\n本实例将退出，不重复登录。", "校园网守护")
		os.Exit(0)
	} else {
		defer release()
	}
	a, err := app.Compose(*cfgPath)
	if err != nil {
		// 首跑可修复的缺口（缺配置/缺凭据）→ 打开配置面板，不让新机托盘
		// 直接报错退出；其余错误仍走 stderr + 非零退出。
		if _, ok := app.AsSetupError(err); !ok {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if !runSetup(*cfgPath) {
			os.Exit(0) // 引导已给（保存后重启），不是错误退出
		}
		a, err = app.Compose(*cfgPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	// 流量越阈：内核给一次性事件，壳层发非阻断的托盘气泡。文案与 Android
	// 通知同源（fluxWarnMessage）；这里只决定 Windows 用什么通道呈现。
	a.Daemon.OnFluxWarn = func(level string, v keeper.FluxUsageView) {
		title, body := fluxWarnMessage(level, v)
		showBalloon(title, body, level == "critical")
	}
	addr, err := a.API.Listen()
	if err != nil {
		fmt.Fprintln(os.Stderr, "回路 API 启动失败:", err)
		os.Exit(2)
	}
	// 运行时端点文件：`cnal status` 据此读真实状态（只读回路，不触发登录）。
	// 正常退出删除；崩溃留下的旧文件会被 status 的 /state 请求判为「未响应」。
	if cleanup, err := app.WriteRuntimeFile(app.RuntimeFilePath(), "http://"+addr.String(), a.API.Token); err != nil {
		fmt.Fprintln(os.Stderr, "端点文件写入失败（cnal status 将不可用）:", err)
	} else {
		defer cleanup()
	}
	a.API.PanelPage = panelPage() // /panel?token=… 面板页（零进程零内存代价）
	if *printURL {
		fmt.Println(a.API.PanelURL())
	}
	if *openPanel {
		// 阻塞到窗口关闭——启动路径放后台，不能挡 systray.Run。
		// panel_mode=browser 时直接走系统浏览器（省 WebView2 常驻内存）。
		mode := panelMode(*cfgPath)
		go func() {
			if err := openPanelByMode(mode, a.API.PanelURL(), openPanelWindow, openURL); err != nil {
				fmt.Fprintln(os.Stderr, "面板打开失败:", err)
			}
		}()
	}
	// 状态迁移文件日志（与 Python keeper.log 同目录，差分回路信号端）。
	// 路径唯一出处 app.LogFilePath：托盘写与面板 /logs 读同一文件。
	// RotateWriter：心跳行 3s/拍 ≈1.6MB/天，10MB+10MB 备份封顶磁盘占用。
	logPath := app.LogFilePath()
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	rw, err := app.NewRotateWriter(logPath, 10*1024*1024)
	if err != nil {
		// 面板「状态日志」依赖这个文件；不弹模态框阻塞启动，但失败必须可见
		// （终端启动时 stderr 可见；GUI 下面板日志会显示「暂无日志」）。
		fmt.Fprintf(os.Stderr, "状态日志不可写 %s: %v（面板日志将报错）\n", logPath, err)
		} else {
			defer rw.Close()
			// 空窗自报（2026-11-13 停搏无痕教训）：上次心跳距现在超 30 分钟即
			// 补一行「心跳空窗」，把停搏从「翻日志才知道」变成「启动即告」。
			// 只读既有文件尾部 8KB，不打探测、不改判定。
			app.AnnotateHeartbeatGap(logPath, rw, time.Now, 30*time.Minute)
			a.Logger = app.NewTransitionLogger(rw, time.Now)
		a.Heart = app.NewHeartbeatLogger(rw, time.Now) // 与迁移行同文件（复盘 §3.1）
		// 门户选路/物理源日志同写状态日志（GUI 无 stdout）：面板 /logs 能看到
		// 「这一拍走了哪条路、为什么」——设计 §8 第一期第 4 项。
		app.SetSourcePrinter(func(format string, args ...any) {
			fmt.Fprintf(rw, "%s INFO %s", time.Now().Format("2006-01-02 15:04:05"),
				fmt.Sprintf(format, args...))
		})
	}
	systray.Run(onTrayReady(a), func() {})
}

// onTrayReady 托盘就绪：初始图标 + 菜单（对齐 Python 守护 7 动作）+ tick 循环。
func onTrayReady(a *app.App) func() {
	return func() {
		cfgAbs, _ := filepath.Abs(cfgPathFlag)
		// 状态行（禁用项：显示 label + detail）
		mState := systray.AddMenuItem("启动中", "")
		mState.Disable()
		mLogin := systray.AddMenuItem("立即检测重连", "手动重连（不受自动闸限制）")
		mPanel := systray.AddMenuItem("打开面板", "打开状态面板（panel_mode=native 时用原生窗口）")
		mBrowserPanel := systray.AddMenuItem("在浏览器打开面板", "用系统浏览器打开（不启动 WebView2，内存更省）")
		mFlux := systray.AddMenuItem("查流量", "查流量（打开面板并自动查询）")
		systray.AddSeparator()
		mPortal := systray.AddMenuItem("打开认证页", "浏览器打开门户登录页")
		mLog := systray.AddMenuItem("查看日志", "打开状态日志文件所在目录")
		mAutoConn := systray.AddMenuItem("掉线自动重连", "勾选=开（关=只探测报状态，不自动登录；手动重连不受限）")
		if a.Daemon.AutoReconnectOn() {
			mAutoConn.Check() // ini auto_reconnect 缺省开；关过则保持不勾
		}
		mAuto := systray.AddMenuItem("开机自启", "切换 HKCU Run 登记项（勾选=已启用）")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "")
		go func() {
			for range mLogin.ClickedCh {
				go func() { _ = a.ManualLogin() }()
			}
		}()
		go func() {
			for range mPanel.ClickedCh {
				// 按 panel_mode 分派：native 用 WebView2 壳，browser 直接开浏览器；
				// native 失败仍回落浏览器。openPanelWindow 阻塞到窗口关闭，必须独占 goroutine。
				go func() {
					if err := openPanelByMode(panelMode(cfgPathFlag), a.API.PanelURL(),
						openPanelWindow, openURL); err != nil {
						fmt.Fprintln(os.Stderr, "面板打开失败:", err)
					}
				}()
			}
		}()
		go func() {
			for range mBrowserPanel.ClickedCh {
				// 显式浏览器入口：无视 panel_mode，一次性的省内存选择。
				_ = openURL(a.API.PanelURL())
			}
		}()
		go func() {
			for range mFlux.ClickedCh {
				// 与「打开面板」同一窗口，URL 带 flux=1 让面板自动查询一次。
				go func() {
					u := a.API.PanelURL() + "&flux=1"
					if err := openPanelByMode(panelMode(cfgPathFlag), u,
						openPanelWindow, openURL); err != nil {
						fmt.Fprintln(os.Stderr, "面板打开失败:", err)
					}
				}()
			}
		}()
		go func() {
			for range mPortal.ClickedCh {
				_ = openURL(a.Cfg.Server + a.Cfg.LoginPage)
			}
		}()
		go func() {
			for range mLog.ClickedCh {
				// 可视化日志：打开面板并定位日志卡（替代直接开文件夹的占位实现）。
				go func() {
					u := a.API.PanelURL() + "&logs=1"
					if err := openPanelByMode(panelMode(cfgPathFlag), u,
						openPanelWindow, openURL); err != nil {
						fmt.Fprintln(os.Stderr, "面板打开失败:", err)
					}
				}()
			}
		}()
		go func() {
			for range mAutoConn.ClickedCh {
				on := !a.Daemon.AutoReconnectOn()
				// 经回路同一缝落 ini + 热生效（与面板开关同一出处，不造第二条路）。
				if err := a.API.SaveConfig(map[string]string{"auto_reconnect": strconv.FormatBool(on)}); err != nil {
					fmt.Fprintln(os.Stderr, "自动重连开关保存失败:", err)
					continue
				}
				if on {
					mAutoConn.Check()
				} else {
					mAutoConn.Uncheck()
				}
			}
		}()
		go func() {
			for range mAuto.ClickedCh {
				if isAutostartEnabled() {
					_ = removeAutostart()
				} else if err := installAutostart(selfExePath(), cfgAbs); err != nil {
					fmt.Fprintln(os.Stderr, "自启登记失败:", err)
				}
				mAuto.Check()
				_ = mAuto
			}
		}()
		go func() {
			<-mQuit.ClickedCh
			systray.Quit()
		}()

			// tick 循环：心跳每拍必记（A1 漏拍测量，复盘 §3.1）；Observe 迁移
			// 才落「状态:」行（与 Python keeper.log 对齐）；Beat 无条件落「拍:」行。
			go func() {
				last := ""
				lastDetail := ""
				lastDegraded := false // 「在线且受限」的边沿记忆（气泡只在进窗时弹一次）
				icons := newIconCache() // 同 (色,形) 只画一次（状态抖动时不重复编码）
				for {
					a.Daemon.Tick()
					state := a.Daemon.StateForUI() // 加锁读，避免与 Tick 写竞态
					_, detail, _, _, _ := a.Daemon.View() // 每拍重读：L2/v6 文案变化不改 state，tooltip 要跟上
					if a.Logger != nil {
						_, detail, _, _, _ := a.Daemon.View()
						a.Logger.Observe(state, detail) // 迁移才写（内部去重）
					}
					if a.Heart != nil {
						_, detail, _, _, _ := a.Daemon.View()
						a.Heart.Beat(state, detail) // 每拍必写（A1 漏拍测量）
					}
					if state != last || detail != lastDetail {
						snap := a.API.Snapshot()
						ico, err := icons.get(snap.Color, shapeForState(state))
						if err != nil {
							// 静默跳过是 2026-11-07 图标空白事故的根因：失败必须可见。
							// cnalw 是 GUI 子系统（无控制台），可见落点 = 状态日志文件。
							msg := fmt.Sprintf("%s WARN 图标生成失败 state=%s color=%s: %v\n",
								time.Now().Format("2006-01-02 15:04:05"), state, snap.Color, err)
							if f, oerr := os.OpenFile(app.LogFilePath(),
								os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); oerr == nil {
								_, _ = f.WriteString(msg)
								_ = f.Close()
							}
						} else {
							systray.SetIcon(ico)
						}
						line := statusLine(snap)
						systray.SetTooltip(line)
						mState.SetTitle(line)
						if a.Daemon.AutoReconnectOn() {
							mAutoConn.Check()
						} else {
							mAutoConn.Uncheck()
						}
						last = state
						lastDetail = detail
					}
					// 风控窗口气泡：只在「在线且受限」的上升沿弹一次——离线/免认证态不发
					//（「认证正常」与实情不符）；恢复后下一个窗口再弹，不是每拍弹。
					restricted := a.Daemon.L2RestrictedOnUI()
					degraded := state == keeper.StateOnline && restricted
					if degraded && !lastDegraded {
						title, body := l2BalloonMessage()
						showBalloon(title, body, false)
					}
					lastDegraded = degraded
					time.Sleep(a.Cfg.Interval)
				}
			}()
	}
}

// shapeForState 状态→形状（颜色不得是唯一载体，DESIGN.md 同表）。
func shapeForState(state string) string {
	switch state {
	case "online":
		return "solid"
	case "offline", "portal_down":
		return "ring"
	case "rejected":
		return "ring_slash"
	default: // free_window / paused / starting
		return "lamp"
	}
}

// selfExePath 当前可执行文件绝对路径（自启登记用）。
func selfExePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	return abs
}
