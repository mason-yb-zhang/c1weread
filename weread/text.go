// XHTML → 纯文本转换，以及 TXT 书原文归一化。
package weread

import (
	"strings"

	"golang.org/x/net/html"
)

// blockTags 在输出中归一为换行边界的块级元素。
var blockTags = map[string]bool{
	"p": true, "div": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "li": true, "ul": true,
	"ol": true, "section": true, "article": true, "header": true,
	"footer": true, "blockquote": true, "table": true, "tr": true,
	"hr": true, "nav": true, "figure": true, "figcaption": true,
	"aside": true, "td": true, "th": true,
}

// HTMLToText 把章节 XHTML 转成规范换行的纯文本：
// 取 body 内容，块级元素归一为换行，noteref 保留 [n]，footnote 就地内联，
// 实体自动解码，行内空白与空行压缩。
func HTMLToText(xhtml string) string {
	doc, err := html.Parse(strings.NewReader(xhtml))
	if err != nil {
		return ""
	}
	body := findElement(doc, "body")
	if body == nil {
		body = doc
	}
	var b strings.Builder
	renderNode(body, &b)
	return normalizeLines(b.String())
}

func findElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func attrValue(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// renderNode 递归渲染：文本原样追加，块级元素包换行，noteref 包 [n]。
func renderNode(n *html.Node, b *strings.Builder) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			b.WriteString(c.Data)
		case html.ElementNode:
			tag := c.Data
			if tag == "script" || tag == "style" {
				continue
			}
			if tag == "br" {
				b.WriteByte('\n')
				continue
			}
			if tag == "img" {
				continue
			}
			if tag == "a" && attrValue(c, "epub:type") == "noteref" {
				b.WriteByte('[')
				renderInline(c, b)
				b.WriteByte(']')
				continue
			}
			if blockTags[tag] {
				b.WriteByte('\n')
				renderNode(c, b)
				b.WriteByte('\n')
			} else {
				renderNode(c, b)
			}
		}
	}
}

// renderInline 渲染元素内的纯文本（用于 noteref 的编号）。
func renderInline(n *html.Node, b *strings.Builder) {
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		renderInline(c, b)
	}
}

// normalizeLines 压缩行内空白并丢弃空行，输出规范 \n 换行文本。
func normalizeLines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	rawLines := strings.Split(s, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = compressInlineSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// compressInlineSpace 行内 ASCII 空白压缩为单空格并去首尾。
func compressInlineSpace(line string) string {
	var b strings.Builder
	b.Grow(len(line))
	pending := false
	started := false
	for _, r := range line {
		if r == '\u00a0' {
			r = ' '
		}
		if r == ' ' || r == '\t' || r == '\f' || r == '\v' {
			if started {
				pending = true
			}
			continue
		}
		if pending {
			b.WriteByte(' ')
			pending = false
		}
		b.WriteRune(r)
		started = true
	}
	return b.String()
}

// NormalizeTXTText 归一 TXT 书原文的换行，保留全角空格缩进。
func NormalizeTXTText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}
