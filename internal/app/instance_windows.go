//go:build windows

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// 单实例互斥（Python keeper.acquire_single_instance 同语义，2026-11-08 移植）：
// 两个守护并行会互抢登录、撞门户反暴力冻结——这是用户实证的"明显不合理"。
// 用 OS 建议锁（LockFileEx 字节锁）而不是"文件存在"判断：持有句柄即持锁，
// 进程无论怎么死（包括强杀）OS 都释放（Python 2026-11-04 踩过残留锁永久
// 阻塞重启的坑，勿改回文件存在判断）。

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock   = 0x00000002
	lockfileFailImmediately = 0x00000001
)

// InstanceLockPath 单实例锁路径：与 Python 的 keeper.lock 分开命名——两套
// 实现不抢同一把锁（换役期间可并存对照，切换完成后只留 Go）。
func InstanceLockPath() string { return filepath.Join(DataDir(), "cnalw.lock") }

// AcquireInstanceLock 获取整机单实例锁（cnalw 与 cnald 共用同一把——两个
// 守护并行会互抢登录撞门户反暴力冻结）。ok=false = 已有实例在跑。
// release 幂等；进程退出时 OS 兜底释放，无需调用方保证。
func AcquireInstanceLock() (release func(), ok bool) {
	p := InstanceLockPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return func() {}, false
	}
	f, ok := acquireInstanceLock(p)
	if !ok {
		return func() {}, false
	}
	return func() { releaseInstanceLock(f) }, true
}

// acquireInstanceLock 尝试独占锁文件。返回 (句柄, true)；已有实例持锁时
// 返回 (nil, false)。句柄必须保持打开（进程存活期间不能关，否则锁释放）。
func acquireInstanceLock(path string) (*os.File, bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	var overlapped syscall.Overlapped
	r, _, _ := procLockFileEx.Call(
		f.Fd(),
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		0, 1, 0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r == 0 {
		_ = f.Close()
		return nil, false
	}
	return f, true
}

// releaseInstanceLock 显式释放（幂等；进程退出时 OS 兜底释放）。
func releaseInstanceLock(f *os.File) {
	if f == nil {
		return
	}
	var overlapped syscall.Overlapped
	_, _, _ = procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	_ = f.Close()
}
