package app

import (
	"os"
	"sync"
)

// RotateWriter 大小上限滚动日志写端（复盘 §3.1）：主档超过 limit 字节时把
// 旧内容挪到 .1（覆盖上一份备份），主档清空续写。对调用方透明（io.Writer），
// TransitionLogger/HeartbeatLogger/SourcePrinter 事件行共用一个写端。
//
// 设计取舍：只保留一份备份（.1）——心跳行 3s/行本身冗余极高，漏拍判定只需
// 最近数小时；状态迁移历史断档可由事件行与更早的 .1 追溯边界弥补。
type RotateWriter struct {
	mu    sync.Mutex
	f     *os.File
	path  string
	limit int
	size  int64
}

// NewRotateWriter 打开（或续写）path；limit 为滚动阈值（字节）。
func NewRotateWriter(path string, limit int) (*RotateWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	// O_APPEND 下自身 offset 不可靠，用文件实际大小续算。
	hi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &RotateWriter{f: f, path: path, limit: limit, size: hi.Size()}, nil
}

// Write 实现 io.Writer；滚动判据在写入前：当前大小 + 本次写入 > limit 即先滚。
func (w *RotateWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(p)) > int64(w.limit) {
		if err := w.rollLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rollLocked 关主档 → 改名为 .1（覆盖旧备份）→ 重开主档。单次写超上限时
// 不再二次滚动（rollLocked 后 size=0，必然直接写入）。
func (w *RotateWriter) rollLocked() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w.f = f
	w.size = 0
	return nil
}

// Close 关闭底层文件（进程退出/测试收尾）。
func (w *RotateWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
