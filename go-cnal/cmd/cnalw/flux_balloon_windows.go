//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows 托盘气泡通知（非阻断、不抢焦点）。
//
// systray 库内部有 NOTIFYICONDATA 与 Shell_NotifyIconW，但 notifyIconData
// 与窗口句柄都不导出，没法在库外复用它的图标发气泡。这里自建一个**独立**
// 通知图标（NIM_ADD → NIF_INFO 气泡 → NIM_DELETE），用固定 GUID 标识，
// 不碰 systray 的主托盘图标；气泡由系统通知中心呈现，不弹 MessageBox、
// 不抢焦点（用户明确不要阻断式弹窗）。
//
// 一次性：事件源各自保证只发一次（流量越阈=keeper fluxWarner 每档一次，
// 风控窗口=托盘 tick 只在上升沿发）；这里不做去重。

var (
	shell32             = syscall.NewLazyDLL("shell32.dll")
	procShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
)

const (
	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010
	nifGuid    = 0x00000020

	niifInfo    = 0x00000001
	niifWarning = 0x00000002
)

// notifyIconDataW 与 Win32 NOTIFYICONDATAW 布局一致（ANSI 版本不用）。
// 字段顺序/大小必须逐字对齐，否则 Shell_NotifyIconW 会读到垃圾。
type notifyIconDataW struct {
	Size            uint32
	Wnd             windows.Handle
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            windows.Handle
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Timeout         uint32
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        windows.GUID
	BalloonIcon     windows.Handle
}

// fluxNotifyGUID 本通知图标的固定标识（与 systray 主图标区分）。
// 用 GUID 而非 Wnd+ID：气泡图标无需消息窗口，NIM_ADD/MODIFY/DELETE 靠 GUID 认。
var fluxNotifyGUID = windows.GUID{
	Data1: 0x8f3c1a2e, Data2: 0x4b7d, Data3: 0x4c91,
	Data4: [8]byte{0xa3, 0x6e, 0xd2, 0x15, 0x7b, 0x08, 0x4e, 0x60},
}

func shellNotifyIcon(msg uintptr, nid *notifyIconDataW) bool {
	r, _, _ := procShellNotifyIcon.Call(msg, uintptr(unsafe.Pointer(nid)))
	return r != 0
}

func utf16Copy(dst []uint16, s string) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	copy(dst, u)
}

// showBalloon 弹一次托盘气泡（流量越阈与风控窗口共用同一通知图标 GUID：
// 同时到达时后发覆盖先发，均为低频事件，可接受）。失败静默——通知本身不该
// 再引发错误链（与 menu.go showMessage 同纪律）。不阻断、不抢焦点。
func showBalloon(title, body string, critical bool) {
	nid := &notifyIconDataW{
		Size:      uint32(unsafe.Sizeof(notifyIconDataW{})),
		Flags:     nifGuid,
		GuidItem:  fluxNotifyGUID,
		Timeout:   10000, // 10s（Win7+ 该字段被系统通知时长策略接管）
		Version:   0,     // NOTIFYICON_VERSION 用 0：气泡由经典 NIF_INFO 呈现
	}
	utf16Copy(nid.Tip[:], "校园网守护")
	if !shellNotifyIcon(nimAdd, nid) {
		return
	}
	nid.Flags = nifGuid | nifInfo
	utf16Copy(nid.Info[:], body)
	utf16Copy(nid.InfoTitle[:], title)
	if critical {
		nid.InfoFlags = niifWarning
	} else {
		nid.InfoFlags = niifInfo
	}
	_ = shellNotifyIcon(nimModify, nid)
	// 不立刻 NIM_DELETE：气泡由系统通知中心排队显示，删太快会看不到。
	// 进程退出时系统自动回收；下一次提醒复用同一 GUID（NIM_MODIFY 覆盖）。
}
