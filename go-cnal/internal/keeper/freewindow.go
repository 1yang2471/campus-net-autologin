package keeper

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Window 一段免认证窗口（分钟数 0-1439；跨午夜窗口 start > end）。
type Window struct{ Start, End int }

// parseWindow 解析单段 "HH:MM-HH:MM"：跨午夜允许（start > end），
// 零长度拒绝；"24:00" 只允许作结束（Python t38 语义）。
func parseWindow(seg string) (Window, error) {
	a, b, ok := strings.Cut(seg, "-")
	if !ok {
		return Window{}, fmt.Errorf("缺少 -（格式 HH:MM-HH:MM）")
	}
	lo, err := parseHM(a)
	if err != nil {
		return Window{}, fmt.Errorf("开始时间 %q 非法（00:00-24:00）", strings.TrimSpace(a))
	}
	hi, err := parseHM(b)
	if err != nil {
		return Window{}, fmt.Errorf("结束时间 %q 非法（00:00-24:00）", strings.TrimSpace(b))
	}
	if lo >= 24*60 {
		return Window{}, fmt.Errorf("开始时间 %q 必须早于 24:00", strings.TrimSpace(a))
	}
	if hi > 24*60 {
		return Window{}, fmt.Errorf("结束时间 %q 不得超过 24:00", strings.TrimSpace(b))
	}
	if lo == hi {
		return Window{}, fmt.Errorf("开始与结束相同（零长度时段）")
	}
	return Window{lo, hi}, nil
}

// ParseFreeWindows 解析 "HH:MM-HH:MM,HH:MM-HH:MM" 配置，非法项跳过不致命
// （移植 Python t38 语义：跨午夜、多段、非法容忍）。运行期读旧配置用它；
// 面板写入前必须走 ValidateFreeWindows（不静默吞掉用户输入）。
func ParseFreeWindows(s string) []Window {
	var ws []Window
	for seg := range strings.SplitSeq(s, ",") {
		if w, err := parseWindow(seg); err == nil {
			ws = append(ws, w)
		}
	}
	return ws
}

// ValidateFreeWindows 严格校验面板提交的免认证时段：空串=禁用；每段必须
// HH:MM-HH:MM、边界合法、非零长度；多段用逗号。错误文案直接给用户看。
func ValidateFreeWindows(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	for i, seg := range strings.Split(s, ",") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			return fmt.Errorf("第 %d 段为空（多段用逗号分隔，末尾不要留逗号）", i+1)
		}
		if _, err := parseWindow(seg); err != nil {
			return fmt.Errorf("第 %d 段 %q 非法：%v", i+1, seg, err)
		}
	}
	return nil
}

// InFreeWindow 判定 minute（0-1439）是否落在任一窗口内；跨午夜窗口
// （start > end）按回绕判定。
func InFreeWindow(ws []Window, minute int) bool {
	for _, w := range ws {
		if w.Start <= w.End {
			if minute >= w.Start && minute <= w.End {
				return true
			}
		} else if minute >= w.Start || minute <= w.End {
			return true
		}
	}
	return false
}

// parseHM 解析 "HH:MM" 为分钟数；越界/非数字报错（"25:00" 非法被跳过）。
func parseHM(s string) (int, error) {
	hs, ms, _ := strings.Cut(strings.TrimSpace(s), ":")
	h, err1 := strconv.Atoi(strings.TrimSpace(hs))
	m, err2 := strconv.Atoi(strings.TrimSpace(ms))
	if err1 != nil || err2 != nil || h < 0 || h > 24 || m < 0 || m > 59 {
		return 0, strconv.ErrSyntax
	}
	return h*60 + m, nil
}

// inFreeWindow 当前（本地）时刻是否在免认证窗口内。
//
// 用注入的 LocalMinute 而不是 time.Now()：gomobile 进程没有 TZ，time.Now()
// 的本地时区退化为 UTC，23:30-06:30（北京时间）会在 UTC 下午被误判成免认证
// 时段，守护据此不自动登录（2026-11-11 M1 首跑真机实证，手机日志打的是 UTC
// 05:57）。安卓由 Kotlin 注入设备偏移；桌面默认缝=系统本地时间。
func (d *Daemon) inFreeWindow() bool {
	d.mu.RLock()
	ws := d.FreeWindows
	d.mu.RUnlock()
	return InFreeWindow(ws, d.LocalMinute())
}

// recheckOnFreeWindowEnd 免认证窗口结束的边沿处理（2026-11-20 排查实证）：
// 窗口内门户不维护会话，结束后首拍若还在 60s 探针节流里，守护会误报在线
// 最多 60 秒。而 Windows 的 NCSI 可能正好在这段"未认证窗口"抓到门户 302，
// 把网络标记成"需要登录"→ 浏览器反复自动打开认证页（用户感知：莫名其妙
// 弹页）。窗口刚结束：作废探针节流，立即核实会话，死则同拍登录。
//
// 调用方传当前拍是否在窗口内；本函数记住该值供下一拍做边沿检测。
func (d *Daemon) recheckOnFreeWindowEnd(inFree bool) {
	if d.wasFreeWindow && !inFree {
		d.sessAt = time.Time{}
		d.sessRedAt = time.Time{}
	}
	d.wasFreeWindow = inFree
}

// SetFreeWindows 热更新免认证时段（面板保存即时生效；Tick 只读锁快照）。
// 传进来的值应由 ValidateFreeWindows 校验过；这里解析仍是容错的。
func (d *Daemon) SetFreeWindows(s string) {
	ws := ParseFreeWindows(s)
	d.mu.Lock()
	d.FreeWindows = ws
	d.freeText = s
	d.mu.Unlock()
}

// FreeWindowsText 当前免认证时段原文（面板回显用；与 FreeWindows 同锁）。
func (d *Daemon) FreeWindowsText() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.freeText
}
