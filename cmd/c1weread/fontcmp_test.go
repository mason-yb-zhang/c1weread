// 候选字体渲染对比：用与运行时完全相同的 TTF→阈值二值化管线，
// 在真实阅读区宽度下渲染 12/14/16px 样张，供挑选正文黑体。
// 仅在设置 C1WEREAD_FONTCMP_DIR（候选 TTF 所在目录）时运行，否则跳过。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"c1device"
)

var fontcmpSamples = []string{
	"　　持续交付是一种软件工程方法，强调小步快跑、频繁验证。作者说：release early, release often——发布频率决定交付质量。",
	"　　本章从三个案例出发，说明为什么「423 读书日」活动让日活提升了 37.5%。数据不会撒谎，1,024 名用户参与了测试。",
	"　　的了一是在人有我他这中大来上国个到说们要为那和地你也子得等着家可下年生出就么之没去自会十着还发成事只作当想作后开出面",
}

// TestFontCompare 渲染候选字体样张到 out/fontcmp/（.frame，需 frame2png 转 PNG 查看）。
func TestFontCompare(t *testing.T) {
	dir := os.Getenv("C1WEREAD_FONTCMP_DIR")
	if dir == "" {
		t.Skip("C1WEREAD_FONTCMP_DIR 未设置")
	}
	candidates := map[string]string{
		"neoxihei": "LXGWNeoXiHei.ttf",
		"misans":   "MiSans-Regular.ttf",
		"wenkai":   "LXGWWenKaiLite-Regular.ttf", // 现网基准，放候选目录里做对照
	}
	sizes := []int{12, 14, 16}

	outDir := filepath.Join("..", "..", "out", "fontcmp")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, file := range candidates {
		path := filepath.Join(dir, file)
		mf, err := openMappedFont(path)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		for _, size := range sizes {
			face, err := mf.typeface.NewFace(float64(size))
			if err != nil {
				t.Errorf("%s %dpx: %v", name, size, err)
				continue
			}
			c := c1device.NewCanvas()
			c.Clear()
			top := 2
			for _, sample := range fontcmpSamples {
				for _, line := range face.Wrap(sample, readerBodyWidth) {
					if top+face.LineHeight() > c1device.DisplayHeight {
						break
					}
					c.DrawTextThreshold(face, 4, top, line, 128)
					top += face.LineHeight()
				}
				top += face.LineHeight() / 2
			}
			frame := c.Frame(128)
			lh := face.LineHeight()
			face.Close()
			out := filepath.Join(outDir, fmt.Sprintf("%s-%dpx.frame", name, size))
			if err := os.WriteFile(out, frame[:], 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s %dpx -> %s (lineHeight=%d)", name, size, out, lh)
		}
		mf.Close()
	}
}
