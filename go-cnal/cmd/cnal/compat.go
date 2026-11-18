package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"cnal/internal/app"
	"cnal/internal/keeper"
)

// cnal compat：离线兼容性自检（Python compat_check.py 同款用法）。
// 默认喂登录响应样本看判定；--detect 喂登录页/抓包样本看协议族。
// 全程不联网、不发凭据；异校词表从守护同款 ini 的 [response] 读。
const compatUsage = `用法:
  cnal compat "<响应样本>" [--config <ini>]
  cnal compat --file response.txt [--config <ini>]
  cnal compat --detect "<登录页样本>" [--config <ini>]
  cnal compat --detect --file login_page.html [--config <ini>]`

func runCompatCmd(args []string) int {
	fs := flag.NewFlagSet("compat", flag.ContinueOnError)
	detect := fs.Bool("detect", false, "协议指纹模式（喂登录页/抓包样本）")
	file := fs.String("file", "", "从文件读取样本（与位置参数二选一）")
	cfgPath := fs.String("config", "", "守护 ini（读 [response] 词表）")
	fs.SetOutput(os.Stderr)
	// Go 的 flag 在第一个位置参数后停止解析，而 README 用法允许
	// "cnal compat <样本> --config <ini>"：先把 flag 挪到前面，再解析。
	if err := fs.Parse(flagsFirst(args, fs)); err != nil {
		return exitCfg
	}
	positional := fs.Args()
	if len(positional) > 1 || (len(positional) == 0 && *file == "") ||
		(len(positional) == 1 && *file != "") {
		fmt.Fprintln(os.Stderr, compatUsage)
		return exitCfg
	}
	sample := ""
	if len(positional) == 1 {
		sample = positional[0]
	}
	sample, err := compatSample(sample, *file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取样本失败:", err)
		return exitCfg
	}
	marks := keeper.DefaultResponseMarks()
	if *cfgPath != "" {
		cfg, cerr := loadConfig(*cfgPath)
		if cerr != nil {
			fmt.Fprintln(os.Stderr, "读配置失败:", cerr)
			return exitCfg
		}
		marks = cfg.Response
	} else if cfg, cerr := loadConfig(app.ConfigFilePath()); cerr == nil {
		// 缺省也吃守护同款 ini：用户校准过 [response] 后自检结论与守护一致。
		marks = cfg.Response
	}
	fmt.Print(compatReport(sample, *detect, marks))
	return exitOK
}

// flagsFirst 把任意位置的 -flag（及其值）重排到位置参数前面。
// 只知道本命令的三个 flag 形态：布尔开关无值，其余取下一个 token 为值；
// 用 fs.Lookup 判断是否需要取值，避免把样本里的 "-x" 当值吃掉。
func flagsFirst(args []string, fs *flag.FlagSet) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			rest = append(rest, a)
			continue
		}
		name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		f := fs.Lookup(name)
		if f == nil {
			rest = append(rest, a) // 未知 flag 交给 flag 包报错
			continue
		}
		flags = append(flags, a)
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue
		}
		if !strings.Contains(a, "=") && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, rest...)
}

// compatSample 位置参数优先，否则读 --file；两者都缺由调用方拦。
func compatSample(arg, path string) (string, error) {
	if arg != "" {
		return arg, nil
	}
	if path == "" {
		return "", errors.New("缺样本")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// compatReport 渲染离线自检结论（纯函数：同一份 keeper 判定链，不另抄词表）。
func compatReport(sample string, detect bool, marks keeper.ResponseMarks) string {
	var b strings.Builder
	if detect {
		r := keeper.ProtocolFingerprint(sample)
		fmt.Fprintf(&b, "协议族: %s\n", r.Family)
		fmt.Fprintf(&b, "结论: %s\n", r.Verdict)
		fmt.Fprintf(&b, "auth_tag: %s\n", yesNo(r.NeedsAuthTag, "有（提交前须从页面取一次性令牌）", "无"))
		fmt.Fprintf(&b, "密码加密标记: %s\n", yesNo(r.PwdEncrypted, "疑似命中", "无"))
		fmt.Fprintf(&b, "建议: %s\n", r.Advice)
		return b.String()
	}
	r := keeper.CompatCheck(sample, marks)
	fmt.Fprintf(&b, "判定: %s\n", r.Verdict)
	fmt.Fprintf(&b, "规则: %s\n", r.Rule)
	fmt.Fprintf(&b, "细节: %s\n", detailOrNone(r.Detail))
	fmt.Fprintf(&b, "瞬态(可重试): %s\n", yesNo(r.Transient, "是", "否"))
	if r.Verdict == "reject" {
		b.WriteString("\n若这与门户实际结果不符，请在 config.ini 的 [response] 段覆盖词表后重试（cnal compat 与守护吃同一份词表）。\n")
	}
	return b.String()
}

func detailOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(无)"
	}
	return s
}

func yesNo(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}
