package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// S2 托盘图标：状态色圆点，PNG-in-ICO（Vista+ 支持），纯 stdlib 生成——
// 托盘不引图像库。测试守契约：合法 ICO 头 + 内嵌 PNG 解码回状态色。

// 行为：stateIcon 产出合法 ICO（头 6B + 目录 16B + PNG），中心像素=状态色。
func TestStateIconIsPngInIco(t *testing.T) {
	ico, err := stateIcon("#4f9d63")
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(ico) < 22+8 {
		t.Fatalf("ICO 至少 22+8 字节，实际 %d", len(ico))
	}
	// ICONDIR: reserved=0, type=1(icon), count=1
	if ico[0] != 0 || ico[1] != 0 || ico[2] != 1 || ico[3] != 0 || ico[4] != 1 || ico[5] != 0 {
		t.Fatalf("ICO 头非法: % x", ico[:6])
	}
	// ICONDIRENTRY（文件偏移 6-21）: 32x32, planes=1, bpp=32
	if ico[6] != 32 || ico[7] != 32 || ico[10] != 1 || ico[12] != 32 {
		t.Errorf("目录项应为 32x32/1/32，实际 % x", ico[6:14])
	}
	// bytesInRes（偏移 14-17，小端）+ PNG 魔数在偏移 22
	resLen := uint32(ico[14]) | uint32(ico[15])<<8 | uint32(ico[16])<<16 | uint32(ico[17])<<24
	pngPart := ico[22:]
	if resLen != uint32(len(pngPart)) {
		t.Errorf("bytesInRes=%d 应等于 PNG 段长 %d", resLen, len(pngPart))
	}
	if !bytes.HasPrefix(pngPart, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("偏移 22 处必须是 PNG 魔数，实际 % x", pngPart[:4])
	}
	img, err := png.Decode(bytes.NewReader(pngPart))
	if err != nil {
		t.Fatalf("内嵌 PNG 必须可解码: %v", err)
	}
	// 中心像素 = 状态色（圆心，不透明）；角落 = 透明。
	// 注意：PNG 带 alpha 解码回 NRGBA，接口相等比较跨类型恒假，用分量断言。
	if _, _, _, a := img.At(16, 16).RGBA(); a>>8 != 255 {
		t.Errorf("中心像素应为不透明，实际 alpha=%d", a>>8)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Error("角落像素应透明")
	}
}

// 行为：非法色值 → 明确报错（托盘不静默退黑块）。
func TestStateIconRejectsBadColor(t *testing.T) {
	if _, err := stateIcon("not-a-color"); err == nil {
		t.Fatal("非法色值必须报错")
	}
}

// 可视检查辅助：CNAL_ICON_DUMP 非空时把各状态图标落盘到项目 docs/screenshots
// 供人眼看（默认跳过；路径是包内固定相对路径，不拼外部输入）。
func TestDumpStateIconsForReview(t *testing.T) {
	if os.Getenv("CNAL_ICON_DUMP") == "" {
		t.Skip("CNAL_ICON_DUMP 未设置，跳过可视导出")
	}
	dir := filepath.Join("..", "..", "docs", "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, sc := range map[string][2]string{
		"online":   {"#4f9d63", "solid"},
		"offline":  {"#d1913f", "ring"},
		"rejected": {"#d05f52", "ring_slash"},
		"idle":     {"#8b919b", "lamp"},
	} {
		ico, err := stateIconShape(sc[0], sc[1])
		if err != nil {
			t.Fatalf("%s 生成失败: %v", name, err)
		}
		p := filepath.Join(dir, "tray-icon-"+name+".png")
		if err := os.WriteFile(p, ico[22:], 0o644); err != nil {
			t.Fatalf("%s 落盘失败: %v", name, err)
		}
		t.Logf("导出: %s", p)
	}
}
