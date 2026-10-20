package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReaderSettingsValidation(t *testing.T) {
	cases := []struct {
		name string
		s    ReaderSettings
		ok   bool
	}{
		{"default", defaultReaderSettings(), true},
		{"unifont-12", ReaderSettings{Font: fontUnifont, Size: 12, Threshold: 128}, true},
		{"unifont-24", ReaderSettings{Font: fontUnifont, Size: 24, Threshold: 128}, true},
		{"fusion-locked-12", ReaderSettings{Font: fontFusion12, Size: 12, Threshold: 128}, true},
		{"fusion-other-size", ReaderSettings{Font: fontFusion12, Size: 14, Threshold: 128}, false},
		{"lxgw", ReaderSettings{Font: fontLXGW, Size: 18, Threshold: 128}, true},
		{"bad-threshold-low", ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 31}, false},
		{"bad-threshold-high", ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 225}, false},
		{"threshold-boundaries", ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 32}, true},
		{"threshold-boundaries-224", ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 224}, true},
		{"bad-font", ReaderSettings{Font: "comic", Size: 16, Threshold: 128}, false},
		{"bad-size", ReaderSettings{Font: fontUnifont, Size: 13, Threshold: 128}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.s.validate()
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestReaderSettingsPersistence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("C1WEREAD_DATA", dir)

	want := ReaderSettings{Font: fontLXGW, Size: 20, Threshold: 160}
	if err := saveReaderSettings(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadReaderSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("loaded %+v, want %+v", got, want)
	}

	// 损坏的 JSON 应回退到默认值并报错。
	if err := os.WriteFile(readerSettingsPath(), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = loadReaderSettings()
	if err == nil {
		t.Fatal("expected error for invalid settings")
	}
	if got != defaultReaderSettings() {
		t.Fatalf("expected default fallback, got %+v", got)
	}
}

func TestNewReaderFaceUnifontSizes(t *testing.T) {
	for _, size := range readerFontSizes {
		settings := ReaderSettings{Font: fontUnifont, Size: size, Threshold: 128}
		face, data, err := newReaderFace(settings)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		face.Close()
		if data != nil {
			data.Close()
		}
	}
}

func TestLXGWFallbackWhenMissing(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("C1WEREAD_FONTS", emptyDir)

	settings := ReaderSettings{Font: fontLXGW, Size: 14, Threshold: 128}
	_, _, err := newReaderFace(settings)
	if err == nil {
		t.Fatal("expected error when lxgw ttf is missing")
	}

	app := &app{}
	if err := app.applyReaderSettings(settings, false); err != nil {
		t.Fatalf("apply should not fail: %v", err)
	}
	if app.settings.Font != fontUnifont {
		t.Fatalf("expected fallback to unifont, got %+v", app.settings)
	}
	if app.bodyFace == nil {
		t.Fatal("expected bodyFace after fallback")
	}
	app.bodyFace.Close()
	if app.fontData != nil {
		app.fontData.Close()
	}
}

func TestPaginateWithUnifont(t *testing.T) {
	face, data, err := newReaderFace(ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 128})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	if data != nil {
		defer data.Close()
	}

	text := "第一章\n" + repeatString("abcdefghijklmnopqrstuvwxyz\n", 50)
	pages, err := paginate(text, face, readerBodyHeight)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 {
		t.Fatal("expected pages")
	}
	first := pages[0]
	if first.Start != 0 {
		t.Fatalf("first page start = %d, want 0", first.Start)
	}
	for i := 1; i < len(pages); i++ {
		if pages[i].Start != pages[i-1].End {
			t.Fatalf("page boundary mismatch: page %d end=%d, page %d start=%d", i-1, pages[i-1].End, i, pages[i].Start)
		}
	}
}

func TestPaginateWithFusion12(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "repos", "reader-lab", "App", "book-reader", "assets", "fusion12.bin")
	if _, err := os.Stat(src); err != nil {
		t.Skip("fusion12.bin not available:", err)
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, fusion12FileName)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("C1WEREAD_FONTS", dir)

	face, mapped, err := newReaderFace(ReaderSettings{Font: fontFusion12, Size: 12, Threshold: 128})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	if mapped != nil {
		defer mapped.Close()
	}

	text := "　　这是一段用于测试 Fusion 12px 点阵分页的中文文本。反复重复以跨越多页。"
	text += repeatString("中文字符测试。", 80)
	pages, err := paginate(text, face, readerBodyHeight)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 2 {
		t.Fatalf("expected multiple pages, got %d", len(pages))
	}
}

func TestPaginateMixedASCII(t *testing.T) {
	face, data, err := newReaderFace(ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 128})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	if data != nil {
		defer data.Close()
	}

	text := "Hello 世界123abc中文测试。"
	pages, err := paginate(text, face, readerBodyHeight)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 {
		t.Fatal("expected at least one page")
	}
	joined := ""
	for _, p := range pages {
		for _, l := range p.Lines {
			joined += l
		}
	}
	// 原文不含换行时，所有文字应被保留。
	cleaned := cleanText(text)
	if joined != cleaned {
		t.Fatalf("roundtrip text mismatch:\n got %q\nwant %q", joined, cleaned)
	}
}

func repeatString(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func cleanText(s string) string {
	return s
}
