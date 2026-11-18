package app

import (
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
)

// 注入缝的验收点：安卓注入的值必须真的到达组合根的各处消费点，不是只存进
// 一个全局变量没人读。每个测试都把功能故意弄坏就会红（见各用例注释）。

type fakeCreds struct {
	mu                     sync.Mutex
	saved                  map[string]string
	load                   string
	legacyUser, legacyPass string
}

func (f *fakeCreds) Load(user string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.saved[user]; ok {
		return p, nil
	}
	return f.load, nil
}

func (f *fakeCreds) LoadLegacy() (string, string, error) {
	return f.legacyUser, f.legacyPass, nil
}

func (f *fakeCreds) Save(user, pass string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saved == nil {
		f.saved = map[string]string{}
	}
	f.saved[user] = pass
	return nil
}

func TestSetCredentialSourceDrivesLoadAndSave(t *testing.T) {
	f := &fakeCreds{saved: map[string]string{"alice": "pw-a"}}
	SetCredentialSource(f)
	t.Cleanup(func() { SetCredentialSource(nil) })

	setIniUsername("alice")
	t.Cleanup(func() { setIniUsername("") })
	u, p, err := loadCredentials()
	if err != nil || u != "alice" || p != "pw-a" {
		t.Fatalf("注入的凭据未生效：u=%q p=%q err=%v", u, p, err)
	}
	// Save 走的是同一个注入源：面板保存账号后下一次 Load 要读得到。
	if err := credentialSource().Save("bob", "pw-b"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Load("bob"); got != "pw-b" {
		t.Fatalf("Save 未落到注入源：%q", got)
	}
}

func TestUninjectedCredentialsFailLoudly(t *testing.T) {
	// Windows 默认源可用，这个用例只在非 Windows 平台有意义；用注入一个
	// 显式报错的源来守「绝不静默返回空密码」这条通用契约。
	SetCredentialSource(failingCreds{})
	t.Cleanup(func() { SetCredentialSource(nil) })
	setIniUsername("alice")
	t.Cleanup(func() { setIniUsername("") })
	if _, _, err := loadCredentials(); err == nil {
		t.Fatal("注入源报错时不应静默当作无凭据继续")
	}
}

type failingCreds struct{}

func (failingCreds) Load(string) (string, error) {
	return "", errors.New("no cred")
}
func (failingCreds) LoadLegacy() (string, string, error) { return "", "", errors.New("no cred") }
func (failingCreds) Save(string, string) error           { return errors.New("no cred") }

func TestSetDataDirDrivesAllPaths(t *testing.T) {
	dir := t.TempDir()
	SetDataDir(dir)
	t.Cleanup(func() { SetDataDir("") })

	// 四个路径函数必须都落在注入目录下，否则安卓的配置/日志/端点文件
	// 会写到 Windows 默认位置（模拟器上看不见，A1/A3 的算术依据就丢了）。
	for name, got := range map[string]string{
		"config.ini": ConfigFilePath(),
		"cnalw.log":  LogFilePath(),
		"api.json":   RuntimeFilePath(),
		"cnalw.lock": InstanceLockPath(),
	} {
		want := filepath.Join(dir, name)
		if got != want {
			t.Errorf("%s：期望 %q，实际 %q", name, want, got)
		}
	}
}

func TestSetPhysicalSourcesDrivesPlumbingAndSnapshot(t *testing.T) {
	v4 := net.ParseIP("10.20.30.40")
	v6 := net.ParseIP("2001:db8::1")
	SetPhysicalSources(PhysicalSources{V4: v4, V6: v6})
	t.Cleanup(func() { SetPhysicalSources(PhysicalSources{}) })

	cfg := defaultAdvancedConfig()
	cfg.Server = "http://portal.example"
	cfg.BindIP = "" // 不手工钉源，注入值才生效
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if got := pl.currentV4(); !got.Equal(v4) {
		t.Errorf("注入的 v4 未进 plumbing：%v", got)
	}
	if got := pl.currentV6(); !got.Equal(v6) {
		t.Errorf("注入的 v6 未进 plumbing：%v", got)
	}
	if got := v6Source(pl.currentV6); !got.Equal(v6) {
		t.Errorf("handshake/注销读到的不是注入的 v6：%v", got)
	}
	// Snapshot 的 V6Src 必须读注入值，不能重新枚举网卡（安卓上会选到 rmnet）。
	d := newDaemon(cfg, pl)
	a := newAPI(&cfg, pl, d)
	if got := a.Snapshot().V6Src; got != v6.String() {
		t.Errorf("面板 V6Src 显示 %q，期望注入的 %q", got, v6.String())
	}
}

func TestV6RotationIsVisibleToSnapshot(t *testing.T) {
	v4 := net.ParseIP("10.20.30.40")
	first := net.ParseIP("2001:db8::1")
	rotated := net.ParseIP("2001:db8::2")
	SetPhysicalSources(PhysicalSources{V4: v4, V6: first})
	t.Cleanup(func() { SetPhysicalSources(PhysicalSources{}) })
	cfg := defaultAdvancedConfig()
	cfg.Server = "http://portal.example"
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	d := newDaemon(cfg, pl)
	a := newAPI(&cfg, pl, d)

	// 临时地址轮换：Kotlin 重新注入，面板显示与握手取源都要跟着变。
	SetPhysicalSources(PhysicalSources{V4: v4, V6: rotated})
	if got := a.Snapshot().V6Src; got != rotated.String() {
		t.Errorf("轮换后面板仍显示旧地址 %q，期望 %q", got, rotated.String())
	}
	if got := v6Source(pl.currentV6); !got.Equal(rotated) {
		t.Errorf("轮换后握手源仍是 %v", got)
	}
}
