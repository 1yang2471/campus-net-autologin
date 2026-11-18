package app

import (
	"os"
	"strings"
	"testing"
)

// rotate 测试（复盘 §3.1）：cnalw.log 每拍一行心跳（3s 节拍 ≈ 28,800 行/天），
// 无限增长必须截断。策略：超大小上限滚动为 .1（保留一份旧档），写端对调用方
// 透明（仍是 io.Writer）。

// 行为：未超限不滚动；超限后旧内容落 .1、新内容进主档；.1 已存在则覆盖。
func TestRotateWriterRollsOverAtLimit(t *testing.T) {
	dir := t.TempDir()
	main := dir + "/cnalw.log"

	w, err := NewRotateWriter(main, 200) // 上限 200 字节
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	defer w.Close()

	// 第一批：两笔 60 字节，共 120 < 200，不滚动
	line1 := strings.Repeat("a", 59) + "\n"
	if _, err := w.Write([]byte(line1)); err != nil {
		t.Fatalf("写第一批失败: %v", err)
	}
	if _, err := w.Write([]byte(line1)); err != nil {
		t.Fatalf("写第二批失败: %v", err)
	}
	if got := readSmall(t, main); len(got) != 120 {
		t.Fatalf("未超限应全部进主档 120 字节，实际 %d", len(got))
	}

	// 超限写：主档 120 + 新 90 = 210 > 200 → 滚动
	line2 := strings.Repeat("b", 89) + "\n"
	if _, err := w.Write([]byte(line2)); err != nil {
		t.Fatalf("写超限批失败: %v", err)
	}
	rolled := readSmall(t, dir+"/cnalw.log.1")
	if rolled != line1+line1 {
		t.Fatalf(".1 应保存旧内容，实际 %q", rolled[:min(len(rolled), 30)])
	}
	if got := readSmall(t, main); got != line2 {
		t.Fatalf("主档应只含新内容，实际 %d 字节", len(got))
	}
}

// 行为：写端对齐 io.Writer 语义——多次小写累计计入大小，单次写本身超过上限
// 也必须落盘（不能丢数据，滚动后写入）。
func TestRotateWriterOversizedSingleWrite(t *testing.T) {
	dir := t.TempDir()
	w, err := NewRotateWriter(dir+"/cnalw.log", 50)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	defer w.Close()

	big := strings.Repeat("x", 200) + "\n"
	if _, err := w.Write([]byte(big)); err != nil {
		t.Fatalf("超限单写失败: %v", err)
	}
	if got := readSmall(t, dir+"/cnalw.log"); got != big {
		t.Fatalf("单次超限写不得丢数据，实际 %d 字节", len(got))
	}
}

func readSmall(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", path, err)
	}
	return string(b)
}
