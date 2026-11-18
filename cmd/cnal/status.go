package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"cnal/internal/api"
	"cnal/internal/app"
)

// cnal status：只读查看运行中守护的状态（回路 GET /state）。
// 守护是状态唯一真源——CLI 不重新探测、不触发登录、不碰凭据。
// 端点从守护启动时写的 api.json 读；文件缺失/请求失败 = 未运行/未响应。

func runStatusCmd(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	runtimePath := fs.String("runtime", "", "运行中守护的端点文件（缺省取数据目录 api.json）")
	if err := fs.Parse(args); err != nil {
		return exitCfg
	}
	path := *runtimePath
	if path == "" {
		path = app.RuntimeFilePath()
	}
	return statusReport(path, os.Stdout, 3*time.Second)
}

// statusReport 读端点 → 查状态 → 打印。out 供测试注入；错误走 stderr。
func statusReport(path string, out io.Writer, timeout time.Duration) int {
	info, err := app.ReadRuntimeFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "守护未运行:", err)
		return exitNoDaemon
	}
	snap, err := fetchStatus(info, timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "守护未响应:", err)
		return exitNoDaemon
	}
	fmt.Fprint(out, formatStatus(snap))
	return exitOK
}

// fetchStatus GET /state（Bearer token）。非 200/坏 JSON 都返回 error，
// 绝不把半截响应当状态打印。
func fetchStatus(info app.RuntimeInfo, timeout time.Duration) (api.Snapshot, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(info.URL, "/")+"/state", nil)
	if err != nil {
		return api.Snapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+info.Token)
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return api.Snapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return api.Snapshot{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var snap api.Snapshot
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&snap); err != nil {
		return api.Snapshot{}, fmt.Errorf("状态响应解析失败: %w", err)
	}
	return snap, nil
}

// formatStatus 人读输出。缺字段显式写未知/不输出——不编造 v6 行或开关状态。
func formatStatus(s api.Snapshot) string {
	var b strings.Builder
	label := s.Label
	if label == "" {
		label = api.Label(s.State) // 词表唯一出处 api，不在 CLI 自抄
	}
	if label == "" {
		label = "启动中" // 首拍前 state="" 的空档，与面板同语义（不得打印空状态）
	}
	fmt.Fprintf(&b, "状态: %s\n", label)
	if s.Detail != "" {
		fmt.Fprintf(&b, "详情: %s\n", s.Detail)
	}
	if s.V6Label != "" {
		fmt.Fprintf(&b, "v6: %s\n", s.V6Label)
	}
	fmt.Fprintf(&b, "v4 源: %s\n", orUnknown(s.V4Src))
	fmt.Fprintf(&b, "v6 源: %s\n", orUnknown(s.V6Src))
	var parts []string
	if v, ok := s.Extra["session_dead"]; ok {
		parts = append(parts, "会话: "+pick(v, "已失效", "正常"))
	}
	if v, ok := s.Extra["free_mode"]; ok {
		// free_mode = 免会话态（锚点绿 + 登录不受理学得），不是 free_windows
		// 免认证时段——两者含义不同，文案不得混用（2026-11-08 实跑抓错）。
		parts = append(parts, "免会话检测: "+pick(v, "已进入", "未触发"))
	}
	if v, ok := s.Extra["auto_reconnect"]; ok {
		parts = append(parts, "自动重连: "+pick(v, "开", "关"))
	}
	if len(parts) > 0 {
		b.WriteString(strings.Join(parts, "  ") + "\n")
	}
	return b.String()
}

func orUnknown(s string) string {
	if s == "" {
		return "（未知）"
	}
	return s
}

// pick 布尔转文案（真值取 on，假值取 off）。
func pick(v bool, on, off string) string {
	if v {
		return on
	}
	return off
}
