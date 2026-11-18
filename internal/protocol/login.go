package protocol

import (
	"errors"
	"net/url"
)

var (
	ErrPeerAbsent = errors.New("门户无 v6 查询入口")
	ErrPeerQuery  = errors.New("对端握手查询失败")
)

// ac_portal 族默认端点（Python DEFAULTS 同值）。端点仍是校事实：异校必须
// 核对 README；默认值只让首跑有一个可验证的起点，不冒充普适事实。
const (
	DefaultLoginPath = "/ac_portal/login.php"
	DefaultLoginPage = "/ac_portal/default/pc.html?tabs=pwd"
)

// PeerProvider 把"值需现场查询"表达成接口，而不是把值塞进配置文件
// （规格 §6：[form] 只承载静态字面量，${dyn} DSL 被明确否决）。
type PeerProvider interface {
	Value() (string, error) // "", ErrPeerAbsent / "", ErrPeerQuery / "2001:…", nil
}

type providerFunc func() (string, error)

func NewPeerProvider(fn func() (string, error)) PeerProvider { return providerFunc(fn) }

func (p providerFunc) Value() (string, error) { return p() }

// Settings 登录构造参数：双栈开关 + 表单模板（零值 = 参考校默认）。
type Settings struct {
	DualStack bool
	Form      Form
}

// BuildLogin 构造登录表单。表单形状来自 Form（默认与 Python 同形；异校用
// login_form 覆盖字段名/操作名/静态参数）；ipv4or6 只在双栈开且现场取到值
// 时携带，取不到（absent）或查询失败（fail）都不带字段——等价于现版纯 v4
// 登录，两种降级的差别由调用方观测。每次登录构造都现场取值（v6 临时地址会
// 轮换，缓存会在地址轮换后绑到失效地址）。
func BuildLogin(user, pass string, s Settings, peer PeerProvider) map[string]string {
	f := s.Form.Fields(user, pass)
	if !s.DualStack {
		return f
	}
	if v, err := peer.Value(); err == nil && v != "" {
		f[reservedFormKey] = v // 现场取值，不缓存
	}
	return f
}

// EncodeForm 把表单编码成 application/x-www-form-urlencoded（键序规范化）。
func EncodeForm(f map[string]string) string {
	vals := url.Values{}
	for k, v := range f {
		vals.Set(k, v)
	}
	return vals.Encode()
}
