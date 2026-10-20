// 视图渲染：书架 / 下载 / 目录 / 阅读 / 引导 / 错误。
package main

import (
	"fmt"
	"image"
	"os"
	"strings"

	"c1device"
)

const (
	rowH        = 16
	titleY      = 0
	contentY    = rowH
	listRows    = 7 // 列表行数（第 8 行留给状态）
	statusY     = contentY + listRows*rowH
	readerRows  = 8 // 阅读器正文行数
	textWidthPx = 288
)

// viewMode 视图状态机。
type viewMode int

const (
	viewGuide viewMode = iota
	viewShelf
	viewDownload
	viewCatalog
	viewReader
	viewSettings
	viewError
)

// guideLines 凭证缺失引导屏内容。
var guideLines = []string{
	"未找到凭证 creds.json",
	"",
	"1. PC 运行 weread-login-win.exe",
	"2. 用微信扫码确认登录",
	"3. adb push creds.json 到",
	"   /storage/c1/local-apps/",
	"   apps/weread/data/",
	"4. 重新启动本应用",
}

// render 按当前视图渲染整帧。
func (a *app) render() c1device.Frame {
	c := c1device.NewCanvas()
	c.Clear()
	switch a.view {
	case viewGuide:
		c.DrawText(a.face, 2, titleY, "微信读书 c1weread")
		drawPlainLines(c, a.face, guideLines)
	case viewShelf:
		a.renderShelf(c)
	case viewDownload:
		a.renderDownload(c)
	case viewCatalog:
		a.renderCatalog(c)
	case viewReader:
		a.renderReader(c)
	case viewSettings:
		a.renderSettings(c)
	case viewError:
		c.DrawText(a.face, 2, titleY, "出错")
		drawPlainLines(c, a.face, wrapStatusLines(a.errMsg))
		c.DrawText(a.face, 2, statusY, "←/Back 返回书架")
	}
	return c.Frame(128)
}

func drawPlainLines(c *c1device.Canvas, face *c1device.Face, lines []string) {
	for i, line := range lines {
		if i >= listRows+1 {
			break
		}
		c.DrawText(face, 2, contentY+i*rowH, line)
	}
}

// renderShelf 书架列表。
func (a *app) renderShelf(c *c1device.Canvas) {
	c.DrawText(a.face, 2, titleY, "书架")
	count := len(a.shelf)
	if count == 0 {
		msg := a.status
		if msg == "" {
			msg = "书架为空"
		}
		c.DrawText(a.face, 2, contentY, msg)
	} else {
		first, sel := a.shelfFirst, a.shelfSel
		for row := 0; row < listRows; row++ {
			idx := first + row
			if idx >= count {
				break
			}
			top := contentY + row*rowH
			rect := image.Rect(0, top, c1device.DisplayWidth, top+rowH)
			book := a.shelf[idx]
			label := " " + book.Title
			if book.Album {
				label += "（有声书·不支持）"
			}
			label = truncateToWidth(a.face, label, c1device.DisplayWidth-4)
			if idx == sel {
				c.DrawInvertedTextBar(a.face, rect, label)
			} else {
				c.DrawText(a.face, 2, top, label)
			}
		}
		c.DrawScrollIndicator(c1device.DisplayWidth-3, contentY, contentY+listRows*rowH, sel, count)
	}
	status := a.status
	if status == "" {
		status = "OK 打开  ↑↓选择"
	}
	c.DrawText(a.face, 2, statusY, status)
}

// renderDownload 下载进度。
func (a *app) renderDownload(c *c1device.Canvas) {
	title := "下载"
	if a.currentBook != nil {
		title = truncateToWidth(a.face, a.currentBook.Title, 200)
	}
	c.DrawText(a.face, 2, titleY, title)
	lines := []string{
		fmt.Sprintf("下载中 %d/%d", a.downloadDone, a.downloadTotal),
		"",
		"每章间隔 200ms，请稍候",
		"Back 取消返回书架",
	}
	drawPlainLines(c, a.face, lines)
	if a.downloadTotal > 0 {
		barW := a.downloadDone * (c1device.DisplayWidth - 20) / a.downloadTotal
		barY := contentY + 4*rowH + 8 // 文字块（4 行）之下，避免与文字重叠
		c.DrawRect(image.Rect(8, barY, c1device.DisplayWidth-8, barY+12))
		if barW > 0 {
			c.FillRect(image.Rect(8, barY, 8+barW, barY+12))
		}
	}
}

// renderCatalog 章节目录。
func (a *app) renderCatalog(c *c1device.Canvas) {
	title := "目录"
	if a.currentBook != nil {
		title = truncateToWidth(a.face, a.currentBook.Title+"·目录", 220)
	}
	c.DrawText(a.face, 2, titleY, title)
	count := len(a.chapters)
	if count == 0 {
		c.DrawText(a.face, 2, contentY, "无章节")
	} else {
		first, sel := a.catFirst, a.catSel
		for row := 0; row < listRows; row++ {
			idx := first + row
			if idx >= count {
				break
			}
			top := contentY + row*rowH
			rect := image.Rect(0, top, c1device.DisplayWidth, top+rowH)
			label := " " + a.chapters[idx].Title
			if _, err := chapterText(a.currentBook, a.chapters[idx]); err != nil {
				label += "（未下载）"
			}
			label = truncateToWidth(a.face, label, c1device.DisplayWidth-4)
			if idx == sel {
				c.DrawInvertedTextBar(a.face, rect, label)
			} else {
				c.DrawText(a.face, 2, top, label)
			}
		}
		c.DrawScrollIndicator(c1device.DisplayWidth-3, contentY, contentY+listRows*rowH, sel, count)
	}
	c.DrawText(a.face, 2, statusY, "OK 阅读  ←返回书架")
}

// renderReader 正文阅读页。
func (a *app) renderReader(c *c1device.Canvas) {
	title := truncateToWidth(a.face, a.readerTitle, c1device.DisplayWidth-40)
	c.DrawText(a.face, 2, titleY, title)
	pageCount := max(1, len(a.pages))
	c.DrawTextRight(a.face, c1device.DisplayWidth-2, titleY,
		fmt.Sprintf("%d/%d", a.readerPage+1, pageCount))
	if len(a.pages) == 0 {
		return
	}
	page := a.pages[a.readerPage]
	lineHeight := a.bodyFace.LineHeight()
	if lineHeight < 1 {
		lineHeight = rowH
	}
	for i, line := range page.Lines {
		top := readerBodyTop + i*lineHeight
		if isTTF(a.settings.Font) {
			c.DrawTextThreshold(a.bodyFace, 4, top, line, uint8(a.settings.Threshold))
		} else {
			c.DrawText(a.bodyFace, 4, top, line)
		}
	}
	if pageCount > 1 {
		c.DrawScrollIndicator(c1device.DisplayWidth-3, readerBodyTop, readerBodyTop+len(page.Lines)*lineHeight, a.readerPage, pageCount)
	}
}

// renderSettings 阅读设置视图。
func (a *app) renderSettings(c *c1device.Canvas) {
	c.DrawText(a.face, 2, titleY, "阅读设置")
	fontLabel := "字体  " + fontDisplayName(a.settings)
	sizeLabel := fmt.Sprintf("字号  %d px", a.settings.Size)
	thresholdLabel := fmt.Sprintf("阈值  %d", a.settings.Threshold)
	if !isTTF(a.settings.Font) {
		thresholdLabel += " (点阵无影响)"
	}
	items := []string{fontLabel, sizeLabel, thresholdLabel}
	hint := "↑↓选择 ←→调整 S/Back 返回"
	if a.message != "" {
		hint = a.message
	}
	for i, item := range items {
		top := contentY + i*rowH
		if i == a.settingsSel {
			rect := image.Rect(0, top, c1device.DisplayWidth, top+rowH)
			c.DrawInvertedTextBar(a.face, rect, truncateToWidth(a.face, "› "+item, c1device.DisplayWidth-4))
		} else {
			c.DrawText(a.face, 2, top, truncateToWidth(a.face, "  "+item, c1device.DisplayWidth-4))
		}
	}
	c.DrawText(a.face, 2, statusY, truncateToWidth(a.face, hint, c1device.DisplayWidth-4))
}

// truncateToWidth 按像素宽截断文本（超宽加 …）。
func truncateToWidth(face *c1device.Face, s string, width int) string {
	if face.Measure(s) <= width {
		return s
	}
	for len(s) > 1 {
		s = s[:len(s)-1]
		if face.Measure(s+"…") <= width {
			return s + "…"
		}
	}
	return "…"
}

// wrapStatusLines 把错误信息按屏宽折行。
func wrapStatusLines(msg string) []string {
	var out []string
	for _, raw := range strings.Split(msg, "\n") {
		line := strings.TrimSpace(raw)
		for line != "" {
			runes := []rune(line)
			w := 0
			cut := 0
			for cut < len(runes) {
				rw := runeAdvance(runes[cut])
				if w+rw > textWidthPx-8 && cut > 0 {
					break
				}
				w += rw
				cut++
			}
			out = append(out, string(runes[:cut]))
			line = strings.TrimSpace(string(runes[cut:]))
		}
	}
	if len(out) == 0 {
		out = []string{"未知错误"}
	}
	return out
}

// chapterText 读一章正文；未下载返回错误（阅读器按需补章）。
func chapterText(m *manifest, ch chapterMeta) (string, error) {
	if m == nil {
		return "", fmt.Errorf("无打开的书")
	}
	data, err := os.ReadFile(chapterFile(m.BookID, ch))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
