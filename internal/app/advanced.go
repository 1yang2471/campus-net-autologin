package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"cnal/internal/keeper"
	"cnal/internal/protocol"
)

// 面板高级设置区：ini 里已有但常用设置卡不展示的字段。
// 单一出处纪律：数值范围与 Python _num 同款；词表落 [response] 段（Python
// read_ini 的口径），绝不混进 [keeper] 段。

// advancedKeeperKeys 数值/开关类键（写 [keeper] 段）及其合法范围（秒）。
// boolean 键单独在 validate 里判。
var advancedKeeperKeys = map[string]struct {
	lo, hi float64
}{
	"interval":          {3, 300}, // 2026-11-13 登录风暴脚枪：下限与加载钳制同源（config.go <3s → 3s）
	"probe_timeout":     {1, 15},
	"login_timeout":     {6, 60},
	"portal_down_wait":  {5, 600},
	"reject_backoff":    {60, 86400},
	"transient_backoff": {5, 600},
}

var advancedTextKeys = map[string]bool{
	"campus_dns": true, "probe_ips": true, "login_page": true,
	"user_agent": true, "ssl_verify": true, "login_form": true,
}

var advancedResponseKeys = map[string]bool{
	"success_marks": true, "negative_marks": true,
	"transient_marks": true, "freeze_marks": true,
}

// advancedConfigView 读高级设置的非敏感视图（密码绝不进视图）。缺配置时
// 回落默认值——面板首跑也能显示，而不是空表单。
func advancedConfigView(path string) map[string]string {
	cfg, err := loadDaemonConfig(path)
	if err != nil {
		cfg = defaultAdvancedConfig()
	}
	return map[string]string{
		"campus_dns":        cfg.CampusDNS,
		"interval":          fmt.Sprint(int(cfg.Interval.Seconds())),
		"probe_timeout":     fmt.Sprint(int(cfg.ProbeTimeout.Seconds())),
		"login_timeout":     fmt.Sprint(int(cfg.LoginTimeout.Seconds())),
		"probe_ips":         strings.Join(cfg.ProbeIPs, ","),
		"login_page":        cfg.LoginPage,
		"user_agent":        cfg.UserAgent,
		"ssl_verify":        strconv.FormatBool(cfg.SSLVerify),
		"portal_down_wait":  fmt.Sprint(int(cfg.PortalDownWait.Seconds())),
		"reject_backoff":    fmt.Sprint(int(cfg.RejectBackoff.Seconds())),
		"transient_backoff": fmt.Sprint(int(cfg.TransientBackoff.Seconds())),
		"login_form":        cfg.LoginForm.Template(),
		"success_marks":     strings.Join(cfg.Response.Success, ","),
		"negative_marks":    strings.Join(cfg.Response.Negative, ","),
		"transient_marks":   strings.Join(cfg.Response.Transient, ","),
		"freeze_marks":      strings.Join(cfg.Response.Freeze, ","),
	}
}

// defaultAdvancedConfig 缺配置时给面板的回落值（与 loadDaemonConfig 的默认值同源）。
func defaultAdvancedConfig() daemonConfig {
	c := daemonConfig{
		ProbeTimeout:     3 * time.Second,
		Interval:         3 * time.Second,
		LoginTimeout:     6 * time.Second,
		PortalDownWait:   30 * time.Second,
		RejectBackoff:    600 * time.Second,
		TransientBackoff: 30 * time.Second,
		SSLVerify:        true,
		Response:         keeper.DefaultResponseMarks(),
		LoginForm:        protocol.DefaultForm(),
	}
	return c
}

// validateAdvancedConfig 在任何写入之前校验：数值范围、布尔开关、词表键
// 白名单、CRLF 注入（值里换行会拆出新的节/键）。
func validateAdvancedConfig(values map[string]string) error {
	for k, v := range values {
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("配置值 %s 含换行，拒绝写入", k)
		}
		switch {
		case advancedKeeperKeys[k].hi > 0:
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fmt.Errorf("%s 不是数字: %w", k, err)
			}
			r := advancedKeeperKeys[k]
			if n < r.lo || n > r.hi {
				return fmt.Errorf("%s 应在 %g–%g 之间，实际 %g", k, r.lo, r.hi, n)
			}
		case k == "ssl_verify":
			if v != "true" && v != "false" {
				return fmt.Errorf("ssl_verify 只能是 true/false，实际 %q", v)
			}
		case k == "login_form":
			if _, err := protocol.ParseForm(v); err != nil {
				return fmt.Errorf("login_form: %w", err)
			}
		case advancedTextKeys[k] || advancedResponseKeys[k]:
			// 文本类：只拦换行（上面已做），其余按原样保存。
		default:
			return fmt.Errorf("不支持的高级配置键: %s", k)
		}
	}
	return nil
}

// saveConfigValues 面板 /config POST 的统一写入分派（组合根与首跑面板共用）：
// 先整批校验，再按常用键/高级键分组写入。任何一组失败都不落第二组——
// 校验先于全部写入，避免「常用键写成功、高级键被拒」的半写。
func saveConfigValues(values map[string]string) error {
	common := map[string]string{}
	advanced := map[string]string{}
	for k, v := range values {
		switch {
		case advancedKeeperKeys[k].hi > 0, advancedTextKeys[k], advancedResponseKeys[k]:
			advanced[k] = v
		default:
			common[k] = v
		}
	}
	if err := validateAdvancedConfig(advanced); err != nil {
		return err
	}
	if v, ok := common["free_windows"]; ok {
		if err := keeper.ValidateFreeWindows(v); err != nil {
			return err
		}
	}
	if len(common) > 0 {
		if err := updateIniFields(common); err != nil {
			return err
		}
	}
	if len(advanced) > 0 {
		return writeAdvancedConfig(advanced)
	}
	return nil
}

// writeAdvancedConfig 分节就地改写：数值/文本键进 [keeper]，词表键进
// [response]。其它节（[accounts] 等）原样保留；[response] 不存在时追加。
func writeAdvancedConfig(values map[string]string) error {
	if err := validateAdvancedConfig(values); err != nil {
		return err
	}
	keeperVals := map[string]string{}
	respVals := map[string]string{}
	for k, v := range values {
		if advancedResponseKeys[k] {
			respVals[k] = v
		} else {
			keeperVals[k] = v
		}
	}
	if len(keeperVals) > 0 {
		if err := updateIniFields(keeperVals); err != nil {
			return err
		}
	}
	if len(respVals) > 0 {
		return updateIniSection("response", respVals)
	}
	return nil
}

// updateIniSection 就地改写指定 ini 段的键值（其余节原样）；段不存在则追加。
func updateIniSection(section string, values map[string]string) error {
	lines, err := readIniLines(cfgPathCached)
	if err != nil {
		return err
	}
	header := "[" + section + "]"
	seen := map[string]bool{}
	inSection := false
	sectionExists := false
	insertAt := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if inSection {
				insertAt = i // 目标段结束：新键插在下一个节头前
				inSection = false
			}
			inSection = trimmed == header
			if inSection {
				sectionExists = true
			}
			continue
		}
		if !inSection || trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, _, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}
		k = strings.TrimSpace(k)
		if v, ok := values[k]; ok {
			lines[i] = k + " = " + v
			seen[k] = true
		}
	}
	var missing []string
	for k, v := range values {
		if !seen[k] {
			missing = append(missing, k+" = "+v)
		}
	}
	if len(missing) == 0 {
		return os.WriteFile(cfgPathCached, []byte(strings.Join(lines, iniEOL(cfgPathCached))), 0o600)
	}
	eol := iniEOL(cfgPathCached)
	if sectionExists {
		// 段已存在：缺键插到该段末尾，绝不新建第二个段头。
		at := insertAt
		if at < 0 {
			at = len(lines)
		}
		out := append([]string{}, lines[:at]...)
		out = append(out, missing...)
		out = append(out, lines[at:]...)
		return os.WriteFile(cfgPathCached, []byte(strings.Join(out, eol)), 0o600)
	}
	// 整份文件没有该段：在文件尾追加一个新段（保留原行尾风格）。
	text := strings.Join(lines, eol)
	if !strings.HasSuffix(text, eol) && text != "" {
		text += eol
	}
	text += header + eol + strings.Join(missing, eol) + eol
	return os.WriteFile(cfgPathCached, []byte(text), 0o600)
}
