package app

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// ini 就地改写（面板设置与首跑探测共用）：只动白名单键，其余节/键原样保留。
// 从 app.go 拆出——单文件 20KB 上限是提交钩子的硬闸（tidy §3）。

// updateIniFields 守护 ini 的就地改写：只动传入的键（username/server/
// free_windows/campus_dns 白名单），其余节与键原样保留。
func updateIniFields(values map[string]string) error {
	allowed := map[string]bool{"username": true, "server": true, "free_windows": true,
		"campus_dns": true, "auto_reconnect": true, "multi_channel": true,
		"bind_ip": true,
		"login_path": true} // login_path 只由首跑探测补默认，面板不提交
	// 面板高级设置区复用同一份键集合：不在这里再抄一遍白名单（漏一个键
	// 会让面板保存显示成功、ini 却没变化）。
	for k := range advancedKeeperKeys {
		allowed[k] = true
	}
	for k := range advancedTextKeys {
		allowed[k] = true
	}
	// 值里出现换行会把一行配置拆成两行（可注入任意键/节头）——写入前拒绝。
	for k, v := range values {
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("配置值 %s 含换行，拒绝写入", k)
		}
	}
	lines, err := readIniLines(cfgPathCached)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") || trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, _, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}
		k = strings.TrimSpace(k)
		if allowed[k] {
			if v, ok := values[k]; ok {
				lines[i] = k + " = " + v
				seen[k] = true
			}
		}
	}
	// ini 里原本没有的键追加到 [keeper] 节尾（首个非 keeper 节头前）。
	var out []string
	inserted := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if !inserted && strings.HasPrefix(t, "[") && t != "[keeper]" {
			for k, v := range values {
				if allowed[k] && !seen[k] {
					out = append(out, k+" = "+v)
					seen[k] = true
				}
			}
			inserted = true
		}
		out = append(out, line)
	}
	if !inserted {
		for k, v := range values {
			if allowed[k] && !seen[k] {
				out = append(out, k+" = "+v)
			}
		}
	}
	return os.WriteFile(cfgPathCached, []byte(strings.Join(out, iniEOL(cfgPathCached))), 0o600)
}

// readIniLines 按行读 ini：统一按 \n 切并剥掉行尾 \r，生产 CRLF 与
// 手工编辑后的 LF 都能吃（2026-11-08 发布审查 P1：按 \r\n 切会让 LF
// 文件整份变成一行，面板改一次设置就毁配置）。
func readIniLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(b), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines, nil
}

// iniEOL 探测原文件换行风格（CRLF/LF），改写时保持一致。
func iniEOL(path string) string {
	b, err := os.ReadFile(path)
	if err == nil && bytes.Contains(b, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// cfgPathCached 由 Compose/NewSetup 写入（配置改写目标）。
var cfgPathCached string
