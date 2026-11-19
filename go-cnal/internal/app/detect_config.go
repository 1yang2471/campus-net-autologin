package app

import (
	"bufio"
	"os"
	"strings"

	"cnal/internal/keeper"
)

// DetectOptionsFromConfig 读首跑探测需要的 ini 键（Android 壳自动探测入口用）。
//
// 宽容解析：文件缺失/坏行一律回落默认，不拦探测——首跑时配置可能还不存在，
// 探测本身就是「把 server 找出来」的手段。bindIP 非空优先（安卓壳注入已绑定
// Wi-Fi 的物理 v4）；否则用 ini 的 bind_ip（桌面 CLI 同语义）。
//
// 只读这些键：campus_dns / probe_ips / probe_path / probe_host / login_page /
// login_page_marks / bind_ip。校验失败的数字类键不在这里处理——探测不依赖它们。
func DetectOptionsFromConfig(path, bindIP string) DetectOptions {
	opt := DetectOptions{BindIP: bindIP}
	if path == "" {
		return opt
	}
	f, err := os.Open(path)
	if err != nil {
		return opt
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "[") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "campus_dns":
			opt.CampusDNS = v
		case "probe_ips":
			opt.ProbeIPs = splitProbeIPs(v)
		case "probe_path":
			opt.ProbePath = v
		case "probe_host":
			opt.ProbeHost = v
		case "login_page":
			opt.LoginPage = v
		case "login_page_marks":
			opt.LoginPageMarks = keeper.SplitMarks(v)
		case "bind_ip":
			if opt.BindIP == "" {
				opt.BindIP = v
			}
		}
	}
	return opt
}

// splitProbeIPs 公网锚点列表（逗号分隔，空项忽略）。
func splitProbeIPs(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
