package weread

import (
	"strings"
	"testing"
)

func TestHTMLToText(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>ch1</title><style>p{color:red}</style></head>
<body>
<h1>第一章　开始</h1>
<div class="wr_chapterContent">
<p>这是<b>加粗</b>和<i>斜体</i>的文字。</p>
<p>第二段，含&nbsp;实体 与   多  余  空 格。</p>
<p><a epub:type="noteref" href="#n1">1</a>正文引用脚注。</p>
<aside epub:type="footnote" id="n1"><p>脚注内容：出版说明。</p></aside>
<ul><li>条目一</li><li>条目二</li></ul>
<img src="https://res.weread.qq.com/wrepub/x" alt="图"/>
<p>a &lt; b &amp;&amp; c &gt; d</p>
</div>
</body></html>`
	got := HTMLToText(xhtml)
	want := strings.Join([]string{
		"第一章　开始",
		"这是加粗和斜体的文字。",
		"第二段，含 实体 与 多 余 空 格。",
		"[1]正文引用脚注。",
		"脚注内容：出版说明。",
		"条目一",
		"条目二",
		"a < b && c > d",
	}, "\n")
	if got != want {
		t.Errorf("HTMLToText mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestHTMLToTextNoBody(t *testing.T) {
	got := HTMLToText("<p>没有 body 的片段</p><p>第二段</p>")
	want := "没有 body 的片段\n第二段"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestHTMLToTextBlankLines(t *testing.T) {
	got := HTMLToText("<p>一</p>\n\n\n<p></p>\n\n<p>二</p>")
	want := "一\n二"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestNormalizeTXTText(t *testing.T) {
	got := NormalizeTXTText("第一段\r\n　　缩进\r\r\n第二段\r第三段")
	want := "第一段\n　　缩进\n\n第二段\n第三段"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
