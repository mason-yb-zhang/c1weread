// 按字体真实度量分页。移植并改编自 reader-lab chapter_pages.go / pager.go。
package main

import (
	"fmt"
	"strings"

	"c1device"
)

const readerBodyWidth = c1device.DisplayWidth - 8
const readerBodyTop = rowH
const readerBodyHeight = c1device.DisplayHeight - rowH - 8

// Page 表示一页正文，Start/End 为章节文本中的字节偏移。
type Page struct {
	Lines []string
	Start int
	End   int
}

type positionedLine struct {
	text       string
	start, end int
}

// paginate 把一段 UTF-8 文本按给定 Face 的度量和固定阅读区高度分成若干页。
func paginate(text string, face *c1device.Face) ([]Page, error) {
	if face == nil {
		return nil, fmt.Errorf("paginate: face is nil")
	}
	lineHeight := face.LineHeight()
	if lineHeight < 1 {
		lineHeight = 1
	}
	perPage := readerBodyHeight / lineHeight
	if perPage < 1 {
		perPage = 1
	}

	lines, err := layoutLines(text, face, readerBodyWidth)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return []Page{{Start: 0, End: len(text)}}, nil
	}

	pages := make([]Page, 0, (len(lines)+perPage-1)/perPage)
	for i := 0; i < len(lines); i += perPage {
		end := i + perPage
		if end > len(lines) {
			end = len(lines)
		}
		page := Page{
			Lines: make([]string, end-i),
			Start: lines[i].start,
			End:   lines[end-1].end,
		}
		for j := i; j < end; j++ {
			page.Lines[j-i] = lines[j].text
		}
		pages = append(pages, page)
	}
	return pages, nil
}

// layoutLines 把文本按行折行并记录每个视觉行在原文中的字节边界。
func layoutLines(text string, face *c1device.Face, width int) ([]positionedLine, error) {
	var lines []positionedLine
	position := 0
	remaining := text
	for len(remaining) > 0 {
		length := len(remaining)
		newline := strings.IndexByte(remaining, '\n')
		if newline >= 0 {
			length = newline + 1
		}
		part := remaining[:length]
		lineStart := position
		lineEnd := position + length

		lineText := strings.TrimSuffix(strings.TrimSuffix(part, "\n"), "\r")
		wrapped := face.Wrap(lineText, width)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}

		segmentStart := lineStart
		for idx, wline := range wrapped {
			segmentEnd := lineEnd
			if idx+1 < len(wrapped) {
				segmentEnd = segmentStart + len([]byte(wline))
			}
			lines = append(lines, positionedLine{text: wline, start: segmentStart, end: segmentEnd})
			segmentStart = segmentEnd
		}

		remaining = remaining[length:]
		position = lineEnd
	}
	return lines, nil
}

// pageIndexAt 返回包含指定字节偏移的页索引，越界时返回最近一页。
func pageIndexAt(pages []Page, offset int) int {
	if len(pages) == 0 {
		return 0
	}
	for i, p := range pages {
		if offset < p.Start {
			if i == 0 {
				return 0
			}
			return i - 1
		}
	}
	return len(pages) - 1
}
