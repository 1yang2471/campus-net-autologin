package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DataDir 守护数据目录（config/log/lock 同一处）。平台默认在
// inject_windows.go / inject_other.go；安卓由 SetDataDir(context.filesDir)
// 覆盖——两处拼接点只有这一个，避免各文件各拼一份路径。
func DataDir() string { return dataDir() }

// ConfigFilePath 守护 ini 默认路径（与 Python 守护同一份 config.ini）。
func ConfigFilePath() string { return filepath.Join(DataDir(), "config.ini") }

// LogFilePath 状态日志路径（唯一出处：cnalw 写、面板读都从这里取，
// 不各自拼路径）。
func LogFilePath() string { return filepath.Join(DataDir(), "cnalw.log") }

// TailLines 读文件最后 n 行（新的在最后），供面板可视化日志。
// 实现纪律：从文件尾按块回溯，不整读——日志长期追加后可能是 MB 级，
// 面板每 5s 轮询一次，整读既慢又浪费。文件不存在返回空（新装未写过日志
// 不是错误，面板显示「暂无日志」即可）。
func TailLines(path string, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const block = 8 << 10
	var buf []byte
	pos := st.Size()
	// 找到第 n+1 个换行（行尾空行不算）。
	for pos > 0 {
		read := int64(block)
		if read > pos {
			read = pos
		}
		pos -= read
		chunk := make([]byte, read)
		if _, err := f.ReadAt(chunk, pos); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		buf = append(chunk, buf...)
		if strings.Count(string(buf), "\n") > n {
			break
		}
	}
	// 归一 CRLF 并取最后 n 个非空行。
	text := strings.ReplaceAll(string(buf), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	var out []string
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			out = append(out, lines[i])
		}
	}
	// 反转回文件顺序。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
