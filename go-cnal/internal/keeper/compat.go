package keeper

import "strings"

// 离线兼容性自检（Python keeper.compat_check / protocol_fingerprint 同语义）：
// 喂一段登录响应或登录页样本，不联网、不发凭据，回答「我校门户的响应会被
// 判成什么」「属不属于 ac_portal 族」。异校据此校准 [response] 词表。

// CompatResult 响应判定结论。Rule 是命中的规则名；Transient 表示瞬态错误
// （短退避可重试，不是硬拒绝）。
type CompatResult struct {
	Verdict   string // success / reject
	Rule      string
	Detail    string
	Transient bool
}

// CompatCheck 用守护同一判定链（JudgeLoginResponse）判响应，再归类命中规则。
// 不联网、不发凭据；词表传空即回落默认。
func CompatCheck(sample string, marks ResponseMarks) CompatResult {
	_, err := JudgeLoginResponse(sample, marks)
	low := strings.ToLower(sample)
	transient := marks.withDefaults().transientMarked(sample)
	if err == nil {
		rule := "词表/短响应正向标记"
		switch {
		case strings.Contains(low, `"success"`) || strings.Contains(low, "ret_code"):
			rule = "success/ret_code 正向"
		case strings.Contains(low, "'success'"):
			rule = "伪 JSON success=true"
		}
		return CompatResult{Verdict: "success", Rule: rule, Detail: ""}
	}
	detail := strings.TrimPrefix(err.Error(), "登录未受理: ")
	switch {
	case detail == "空响应":
		return CompatResult{Verdict: "reject", Rule: "空响应", Detail: detail}
	case strings.Contains(detail, "无法判定"):
		return CompatResult{Verdict: "reject", Rule: "长响应无强标记（无法判定）", Detail: detail, Transient: true}
	case transient:
		return CompatResult{Verdict: "reject", Rule: "瞬态错误标记", Detail: detail, Transient: true}
	default:
		return CompatResult{Verdict: "reject", Rule: "负向词/服务器拒绝", Detail: detail}
	}
}

// FingerprintResult 协议族指纹结论。
type FingerprintResult struct {
	Family       string // ac_portal / eportal / srun / unknown
	Verdict      string // compatible / variant_unsupported / other_protocol / unknown
	NeedsAuthTag bool
	PwdEncrypted bool
	Advice       string
}

// 协议族特征（与 Python FAMILY_MARKERS 同源；字典序即判定优先级）。
// Srun 新版端点（get_challenge / rad_user_info / srun_portal）是公开抓包
// 实证，不含 "srun" 字样——补上才能让离线自检正确报 other_protocol。
var familyMarkers = []struct {
	name  string
	marks []string
}{
	{"eportal", []string{"interface.do", "eportal", "drcom", "ddddd=", "0mkkey"}},
	{"srun", []string{"srun", "get_challenge", "rad_user_info"}},
}

const authTagMark = "auth_tag"

var pwdEncryptMarks = []string{"md5", "sha1", "sha256", "encrypt", "rsa", "aes"}

// ProtocolFingerprint 登录页/抓包样本 → 协议族 + 适用性三态结论（离线，零凭据）。
// 朴素 ac_portal（明文密码、无 auth_tag）→ compatible；变体（auth_tag/密码
// 加密）→ variant_unsupported；eportal/srun 等异协议 → other_protocol。
func ProtocolFingerprint(sample string) FingerprintResult {
	low := strings.ToLower(sample)
	if strings.Contains(low, "ac_portal") || strings.Contains(low, "pwdlogin") {
		needsTag := strings.Contains(low, authTagMark)
		pwdEnc := containsAny(low, pwdEncryptMarks)
		if needsTag || pwdEnc {
			return FingerprintResult{
				Family: "ac_portal", Verdict: "variant_unsupported",
				NeedsAuthTag: needsTag, PwdEncrypted: pwdEnc,
				Advice: "ac_portal 变体：提交前需取 auth_tag（一次性令牌）或对密码加密后再 POST——流程级差异，改 config.ini 无法适配，本工具暂不支持。",
			}
		}
		return FingerprintResult{
			Family: "ac_portal", Verdict: "compatible",
			Advice: "朴素 ac_portal（明文密码、无 auth_tag）：按 README「异校适配清单」填 server/campus_dns 即可试用，再用 cnal compat 验证响应判定。",
		}
	}
	for _, f := range familyMarkers {
		if containsAny(low, f.marks) {
			return FingerprintResult{
				Family: f.name, Verdict: "other_protocol",
				Advice: "非 ac_portal 门户（" + f.name + " 系），本工具不适用；请另寻对应协议的自动化工具。",
			}
		}
	}
	return FingerprintResult{
		Family: "unknown", Verdict: "unknown",
		Advice: "样本中未识别出已知门户特征。请确认抓的是登录页或登录 POST（应含 ac_portal/login.php 与 opr=pwdLogin 字样），或对照 README「适用范围与异校适配」。",
	}
}

func containsAny(s string, marks []string) bool {
	for _, m := range marks {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
