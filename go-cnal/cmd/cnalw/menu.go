package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// 托盘动作的实现缝：开机自启（HKCU Run，与 scripts/install-autostart.ps1
// 同键名同值形态）与打开认证页（系统默认浏览器，仅 http/https）。
// 自启键名全局唯一，测试装/卸不碰其他条目。

const autostartValueName = "CampusNetAutoLogin-Go"
const autostartRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// messageBoxW 弹一个原生提示框（GUI 子系统无控制台，退出类消息必须可见）。
var (
	user32       = syscall.NewLazyDLL("user32.dll")
	procMessageW = user32.NewProc("MessageBoxW")
)

// showMessage 同步弹窗（MB_OK|MB_ICONINFORMATION=0x40）；失败静默——
// 提示框本身不该再引发错误链。
func showMessage(text, caption string) {
	t, err1 := syscall.UTF16PtrFromString(text)
	c, err2 := syscall.UTF16PtrFromString(caption)
	if err1 != nil || err2 != nil {
		return
	}
	_, _, _ = procMessageW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x40)
}

// installAutostart 登记开机自启："<exe>" --config "<cfg>"（带引号防空格路径）。
func installAutostart(exePath, cfgPath string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 Run 键失败: %w", err)
	}
	defer k.Close()
	v := fmt.Sprintf(`"%s" --config "%s"`, exePath, cfgPath)
	return k.SetStringValue(autostartValueName, v)
}

// removeAutostart 卸自启；本就未登记不算错。
func removeAutostart() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 Run 键失败: %w", err)
	}
	defer k.Close()
	if err := k.DeleteValue(autostartValueName); err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	return nil
}

// isAutostartEnabled 读勾选态（值存在且指向存在的 exe 才算启用）。
func isAutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(autostartValueName)
	if err != nil || v == "" {
		return false
	}
	// 值形态 "exe" --config "cfg"；取第一段引号里的 exe 验存在。
	if len(v) > 1 && v[0] == '"' {
		if end := indexByte(v[1:], '"'); end >= 0 {
			_, err := os.Stat(v[1 : 1+end])
			return err == nil
		}
	}
	return true // 形态不符时保守报已启用（用户能看到并手动处理）
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// openURL 用系统默认程序打开目标：http/https 走浏览器，存在的目录走资源
// 管理器（ShellExecute verb=open，不经 shell 拼接）。其他一律拒绝——配置项
// 与菜单动作不得变成执行任意文件的口。
func openURL(raw string) error {
	isHTTP := len(raw) > 4 && raw[:4] == "http"
	isDir := false
	if !isHTTP {
		if st, err := os.Stat(raw); err == nil && st.IsDir() {
			isDir = true
		}
	}
	if !isHTTP && !isDir {
		return fmt.Errorf("只允许 http/https 或存在的目录: %q", raw)
	}
	var verbPtr, filePtr *uint16
	var err error
	if verbPtr, err = syscall.UTF16PtrFromString("open"); err != nil {
		return err
	}
	if filePtr, err = syscall.UTF16PtrFromString(raw); err != nil {
		return err
	}
	r1, _, callErr := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		0,
		0,
		uintptr(1), // SW_SHOWNORMAL
	)
	if uintptr(r1) <= 32 {
		return fmt.Errorf("打开失败: %v", callErr)
	}
	return nil
}

var shellExecuteW = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
