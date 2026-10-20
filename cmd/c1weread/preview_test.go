package main

import (
	"os"
	"path/filepath"
	"testing"

	"c1device"
	"c1weread/weread"
)

// TestPreview 渲染各视图到 .frame 供 PC 侧转 PNG 检查。
func TestPreview(t *testing.T) {
	face, err := c1device.NewBitmapFace(fontData, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()

	chapters := []chapterMeta{
		{UID: "1001", Idx: 1, Title: "第一章 投名状"},
		{UID: "1002", Idx: 2, Title: "第二章 转机"},
		{UID: "1003", Idx: 3, Title: "第三章 长夜"},
		{UID: "1004", Idx: 4, Title: "第四章 破晓"},
		{UID: "1005", Idx: 5, Title: "第五章 归途"},
		{UID: "1006", Idx: 6, Title: "第六章 远方"},
		{UID: "1007", Idx: 7, Title: "第七章 回声"},
		{UID: "1008", Idx: 8, Title: "第八章 终章"},
	}

	readerText := "　　持续交付是一种软件工程方法，强调小步快跑、频繁验证。本章从三个案例出发，说明为什么发布频率决定了软件交付的质量与速度。"
	readerPages, err := paginate(readerText, face)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		app  *app
	}{
		{"shelf", &app{
			face: face,
			view: viewShelf,
			shelf: []weread.ShelfBook{
				{BookID: "43208843", Title: "持续交付2.0", Author: "乔梁"},
				{BookID: "32212941", Title: "中医许阳", Author: "唐甲"},
				{BookID: "907755", Title: "三体全集", Author: "刘慈欣"},
				{BookID: "al001", Title: "有声书示例", Author: "某主播", Album: true},
				{BookID: "b5", Title: "格致余论", Author: "朱丹溪"},
				{BookID: "b6", Title: "论语新解", Author: "钱穆"},
				{BookID: "b7", Title: "万历十五年", Author: "黄仁宇"},
				{BookID: "b8", Title: "局外人", Author: "加缪"},
				{BookID: "b9", Title: "鼠疫", Author: "加缪"},
			},
			shelfSel:   1,
			shelfFirst: 0,
		}},
		{"catalog", &app{
			face:        face,
			view:        viewCatalog,
			currentBook: &manifest{BookID: "43208843", Title: "持续交付2.0", Chapters: chapters},
			chapters:    chapters,
			catSel:      2,
			catFirst:    1,
		}},
		{"reader", &app{
			face:        face,
			bodyFace:    face,
			view:        viewReader,
			currentBook: &manifest{BookID: "43208843", Title: "持续交付2.0", Chapters: chapters},
			chapters:    chapters,
			readerTitle: "持续交付2.0·第二章 转机",
			pages:       readerPages,
			readerPage:  0,
			settings:    defaultReaderSettings(),
		}},
		{"settings", &app{
			face:        face,
			bodyFace:    face,
			view:        viewSettings,
			currentBook: &manifest{BookID: "43208843", Title: "持续交付2.0"},
			settings:    defaultReaderSettings(),
			settingsSel: 1,
		}},
		{"error", &app{
			face:   face,
			view:   viewError,
			errMsg: "下载章节「第二章 转机」失败: weread: errCode=-2012 errMsg=登录超时",
		}},
		{"download", &app{
			face:              face,
			view:              viewDownload,
			currentBook:       &manifest{BookID: "43208843", Title: "持续交付2.0"},
			downloadTotal:     20,
			downloadDone:      7,
			downloadCancelled: false,
		}},
	}

	// 若运行时字体目录存在霞鹜文楷，则额外渲染一帧 lxgw 14px 阅读视图；
	// 缺失时自动回退，测试不失败。
	lxgwSettings := ReaderSettings{Font: fontLXGW, Size: 14, Threshold: 128}
	lxgwFace, lxgwData, err := newReaderFace(lxgwSettings)
	if err == nil {
		defer lxgwFace.Close()
		if lxgwData != nil {
			defer lxgwData.Close()
		}
		lxgwPages, err := paginate(readerText, lxgwFace)
		if err == nil {
			cases = append(cases, struct {
				name string
				app  *app
			}{
				"reader-lxgw",
				&app{
					face:        face,
					bodyFace:    lxgwFace,
					view:        viewReader,
					currentBook: &manifest{BookID: "43208843", Title: "持续交付2.0", Chapters: chapters},
					chapters:    chapters,
					readerTitle: "持续交付2.0·第二章 转机",
					pages:       lxgwPages,
					readerPage:  0,
					settings:    lxgwSettings,
				},
			})
		}
	}

	outDir := filepath.Join("..", "..", "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		frame := tc.app.render()
		if len(frame) != c1device.FrameBytes {
			t.Errorf("%s: frame = %d bytes, want %d", tc.name, len(frame), c1device.FrameBytes)
		}
		path := filepath.Join(outDir, "preview-"+tc.name+".frame")
		if err := os.WriteFile(path, frame[:], 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
