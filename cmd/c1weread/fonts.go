// 字体引擎：Unifont 内嵌点阵 / Fusion 12px / 霞鹜文楷 TTF。
// 移植并改编自 reader-lab（GPL-3.0）。
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode"

	"c1device"
)

const (
	fontUnifont  = "unifont"
	fontFusion12 = "fusion12"
	fontLXGW     = "lxgw"

	lxgwFileName    = "LXGWWenKaiLite-Regular.ttf"
	fusion12FileName = "fusion12.bin"

	maxFontBytes = 16 << 20
)

var readerFontSizes = []int{12, 14, 16, 18, 20, 24}

// ReaderSettings 阅读字体设置，持久化到 data/reader-settings.json。
type ReaderSettings struct {
	Font      string `json:"font"`
	Size      int    `json:"size"`
	Threshold int    `json:"threshold"`
}

func defaultReaderSettings() ReaderSettings {
	return ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 128}
}

func validReaderFontName(name string) bool {
	switch name {
	case fontUnifont, fontFusion12, fontLXGW:
		return true
	}
	return false
}

func (s ReaderSettings) validate() error {
	if !validReaderFontName(s.Font) {
		return fmt.Errorf("无效字体")
	}
	okSize := false
	for _, size := range readerFontSizes {
		if s.Size == size {
			okSize = true
			break
		}
	}
	if !okSize {
		return fmt.Errorf("字号仅支持 12/14/16/18/20/24")
	}
	if s.Font == fontFusion12 && s.Size != 12 {
		return fmt.Errorf("Fusion 原生点阵固定 12px")
	}
	if s.Threshold < 32 || s.Threshold > 224 {
		return fmt.Errorf("黑白阈值须在 32 至 224 之间")
	}
	return nil
}

func readerSettingsPath() string { return filepath.Join(dataDir(), "reader-settings.json") }

func loadReaderSettings() (ReaderSettings, error) {
	path := readerSettingsPath()
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultReaderSettings(), nil
		}
		return defaultReaderSettings(), err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return defaultReaderSettings(), err
	}
	if len(data) > 4096 {
		return defaultReaderSettings(), fmt.Errorf("阅读设置文件过大")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var loaded ReaderSettings
	if err := decoder.Decode(&loaded); err != nil {
		return defaultReaderSettings(), fmt.Errorf("解析阅读设置: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return defaultReaderSettings(), fmt.Errorf("阅读设置包含多余内容")
	}
	if err := loaded.validate(); err != nil {
		return defaultReaderSettings(), err
	}
	return loaded, nil
}

func saveReaderSettings(s ReaderSettings) error {
	if err := s.validate(); err != nil {
		return err
	}
	return atomicWrite(readerSettingsPath(), s)
}

// mappedFont 持有通过 mmap（Linux）或普通读取（host）加载的字体字节。
// Face 借用其中的 typeface，关闭 Face 前不可释放 mappedFont。
type mappedFont struct {
	typeface *c1device.Typeface
	data     []byte
}

func openMappedFont(path string) (*mappedFont, error) {
	if err := c1device.RequireStoragePath(path); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maxFontBytes {
		return nil, fmt.Errorf("字体须为不超过 16MiB 的普通文件")
	}
	file, err := openFontFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Size() != before.Size() {
		return nil, fmt.Errorf("字体文件在打开时发生变化")
	}
	data, err := loadFontData(file, int(info.Size()))
	if err != nil {
		return nil, err
	}
	mapped := &mappedFont{data: data}
	typeface, err := c1device.ParseTypeface(data)
	if err != nil {
		mapped.Close()
		return nil, err
	}
	mapped.typeface = typeface
	return mapped, nil
}

func (m *mappedFont) Close() error {
	if m == nil || m.data == nil {
		return nil
	}
	if err := releaseFontData(m.data); err != nil {
		return err
	}
	m.typeface = nil
	m.data = nil
	return nil
}

// fontLineHeight 返回点阵字体缩放后的行高；TTF 使用其自身 metrics。
func fontLineHeight(size int) int {
	return size + (size+3)/4
}

// readerFontsDir 按优先级定位运行时字体目录：
//   1. /proc/self/exe 所在目录的 ../assets/fonts（即 payload/bin/../assets/fonts）
//   2. 同目录的 ./assets/fonts（本地开发/测试布局）
//   3. C1WEREAD_FONTS 环境变量
func readerFontsDir() string {
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		for _, rel := range []string{"..", "."} {
			candidate := filepath.Clean(filepath.Join(dir, rel, "assets", "fonts"))
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}
	}
	if env := os.Getenv("C1WEREAD_FONTS"); env != "" {
		return env
	}
	return ""
}

// newReaderFace 按设置创建正文 Face。返回的 *mappedFont（如有）必须随 Face 一起释放。
func newReaderFace(settings ReaderSettings) (*c1device.Face, *mappedFont, error) {
	if err := settings.validate(); err != nil {
		return nil, nil, err
	}
	switch settings.Font {
	case fontUnifont:
		lh := fontLineHeight(settings.Size)
		if settings.Size == 16 {
			face, err := c1device.NewBitmapFace(fontData, lh)
			return face, nil, err
		}
		face, err := c1device.NewScaledBitmapFace(fontData, settings.Size, lh)
		return face, nil, err
	case fontFusion12:
		path := filepath.Join(readerFontsDir(), fusion12FileName)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("加载 Fusion12: %w", err)
		}
		face, err := c1device.NewPixel12Face(data)
		return face, nil, err
	case fontLXGW:
		path := filepath.Join(readerFontsDir(), lxgwFileName)
		mapped, err := openMappedFont(path)
		if err != nil {
			return nil, nil, fmt.Errorf("加载 %s: %w", lxgwFileName, err)
		}
		face, err := mapped.typeface.NewFace(float64(settings.Size))
		if err != nil {
			mapped.Close()
			return nil, nil, err
		}
		return face, mapped, nil
	}
	return nil, nil, fmt.Errorf("未知字体 %q", settings.Font)
}

func fontDisplayName(settings ReaderSettings) string {
	switch settings.Font {
	case fontUnifont:
		return "Unifont"
	case fontFusion12:
		return "Fusion 12"
	case fontLXGW:
		return "霞鹜文楷"
	}
	return settings.Font
}

func isTTF(font string) bool { return font == fontLXGW }

// fallbackMessage 清理文件名中的控制字符，用于状态提示。
func sanitizeMessage(s string) string {
	out := make([]rune, 0, len([]rune(s)))
	for _, r := range s {
		if !unicode.IsControl(r) {
			out = append(out, r)
		}
	}
	return string(out)
}
