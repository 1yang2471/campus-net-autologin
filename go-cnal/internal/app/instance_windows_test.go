//go:build windows

package app

import (
	"path/filepath"
	"testing"
)

// 单实例互斥（2026-11-08 用户实证：能同时开两个守护 → 互抢登录撞门户反暴力
// 冻结）。Python 用 msvcrt.locking 建议锁；Go 用 LockFileEx 字节锁，语义相同：
// 持有文件句柄即持锁，进程无论怎么死 OS 都释放（不用「文件存在」判断——
// 强杀后残留会永久阻塞重启，Python 2026-11-04 已踩过）。

// 行为：首次获取成功；同进程再获取失败（模拟第二实例）；释放后可重新获取。
func TestInstanceLockMutualExclusion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cnalw.lock")

	f1, ok1 := acquireInstanceLock(p)
	if !ok1 {
		t.Fatal("首次获取应成功")
	}
	if _, ok2 := acquireInstanceLock(p); ok2 {
		t.Error("已持有锁时第二实例必须被拒（否则两个守护并行）")
	}
	releaseInstanceLock(f1)

	f3, ok3 := acquireInstanceLock(p)
	if !ok3 {
		t.Error("释放后应可重新获取（进程死后 OS 释放锁的重启路径）")
	}
	releaseInstanceLock(f1) // 重复释放幂等
	releaseInstanceLock(f3)
}

// 行为：锁文件不存在时自动创建（目录缺失不 panic，返回失败让调用方报错）。
func TestInstanceLockMissingDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "no-such-dir", "x.lock")
	if _, ok := acquireInstanceLock(p); ok {
		t.Error("目录缺失应获取失败而不是崩溃")
	}
}
