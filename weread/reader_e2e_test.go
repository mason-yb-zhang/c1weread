package weread

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// encodeForTest 构造分片编码（decodeEncodedPayload 的逆），与参考 Python 编码侧一致。
func encodeForTest(plain string) string {
	b64 := base64.StdEncoding.EncodeToString([]byte(plain))
	s := strings.NewReplacer("+", "-", "/", "_").Replace(b64)
	positions := swapPositions(s)
	chars := []byte(s)
	for i := 0; i+1 < len(positions); i += 2 {
		for k := 0; k <= 1; k++ {
			left, right := positions[i]+k, positions[i+1]+k
			if left < len(chars) && right < len(chars) {
				chars[left], chars[right] = chars[right], chars[left]
			}
		}
	}
	return "A" + string(chars)
}

func TestEncodeForTestRoundtrip(t *testing.T) {
	for _, plain := range []string{
		"<p>第一章 你好 world</p>",
		"短",
		"一段比较长的文本，用来验证编码解码互逆。包含 unicode：中文、é、🎉。",
	} {
		encoded := encodeForTest(plain)
		got, err := decodeEncodedPayload(encoded)
		if err != nil {
			t.Errorf("roundtrip %q returned error: %v", plain, err)
		} else if got != plain {
			t.Errorf("roundtrip failed: %q -> %q", plain, got)
		}
	}
}

// newTestServer 构造假 weread 服务端（EPUB 书 / TXT 书）。
// 分片遵循真实协议：整段负载编码后切成多片。
func newTestServer(t *testing.T, bookID, psvts, xhtml, txt string) *httptest.Server {
	shards := func(payload string) []string {
		encoded := encodeForTest(payload)
		half := len(encoded) / 2
		return []string{
			shardWithMD5(encoded[:half]),
			shardWithMD5(encoded[half:]),
			shardWithMD5(""),
		}
	}
	var epubShards, txtShards []string
	if xhtml != "" {
		epubShards = shards(xhtml)
	}
	if txt != "" {
		txtShards = shards(txt)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/web/reader/", func(w http.ResponseWriter, r *http.Request) {
		state := map[string]any{
			"reader": map[string]any{
				"bookInfo": map[string]any{"bookId": bookID, "title": "测试书", "author": "作者甲"},
				"psvts":    psvts,
				"token":    "tok123",
			},
		}
		data, _ := json.Marshal(state)
		fmt.Fprintf(w, `<html><head><title>reader</title></head><body><script>window.__INITIAL_STATE__ = %s;(function(){})()</script></body></html>`, data)
	})
	mux.HandleFunc("/web/book/chapterInfos", func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"data": []any{map[string]any{
				"bookId": bookID,
				"updated": []any{
					map[string]any{"chapterUid": 1001, "chapterIdx": 1, "title": "封面", "wordCount": 3},
					map[string]any{"chapterUid": 1002, "chapterIdx": 2, "title": "第一章", "wordCount": 5000},
					map[string]any{"chapterUid": 1003, "chapterIdx": 3, "title": "第二章", "wordCount": 0},
				},
			}},
		}
		data, _ := json.Marshal(payload)
		w.Write(data)
	})
	mux.HandleFunc("/web/book/chapter/e_0", func(w http.ResponseWriter, r *http.Request) {
		if txt != "" {
			// TXT 书：e_0 返回章节元数据 JSON
			w.Write([]byte(fmt.Sprintf(`{"bookId":"%s","format":"txt"}`, bookID)))
			return
		}
		w.Write([]byte(epubShards[0]))
	})
	mux.HandleFunc("/web/book/chapter/e_1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(epubShards[1]))
	})
	mux.HandleFunc("/web/book/chapter/e_3", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(epubShards[2]))
	})
	mux.HandleFunc("/web/book/chapter/t_0", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(txtShards[0]))
	})
	mux.HandleFunc("/web/book/chapter/t_1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(txtShards[1]))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestClient(srv *httptest.Server) *Client {
	jar := NewJar()
	jar.Set("wr_vid", "1")
	jar.Set("wr_skey", "2")
	jar.Set("wr_fp", "123456")
	c := NewClient(jar, nil)
	c.HTTP = srv.Client()
	c.HTTP.Timeout = 0
	c.BaseURL = srv.URL
	return c
}

func TestFetchChapterTextEPUB(t *testing.T) {
	xhtml := `<html xmlns="http://www.w3.org/1999/xhtml"><body><div><p>第一段正文内容。</p><p>第二段&nbsp;带实体。</p></div></body></html>`
	srv := newTestServer(t, "43208843", "psvts-abc", xhtml, "")
	c := newTestClient(srv)

	text, format, err := c.FetchChapterText("43208843", "1002")
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatEPUB {
		t.Errorf("format = %q, want epub", format)
	}
	want := "第一段正文内容。\n第二段 带实体。"
	if text != want {
		t.Errorf("text = %q, want %q", text, want)
	}

	// 章节表
	infos, err := c.FetchChapterInfos("43208843")
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].ChapterUID != "1002" || infos[0].Title != "第一章" {
		t.Errorf("infos = %+v", infos)
	}

	// reader state
	state, err := c.FetchReaderState("43208843", "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Psvts != "psvts-abc" || state.BookID != "43208843" || state.BookTitle != "测试书" {
		t.Errorf("state = %+v", state)
	}
}

func TestFetchChapterTextTXT(t *testing.T) {
	txt := "第一章\r\n　　全文第一段。\r\n　　第二段。"
	srv := newTestServer(t, "32212941", "psvts-xyz", "", txt)
	c := newTestClient(srv)

	text, format, err := c.FetchChapterText("32212941", "1002")
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatTXT {
		t.Errorf("format = %q, want txt", format)
	}
	want := "第一章\n　　全文第一段。\n　　第二段。"
	if text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}

// TestFetchChapterTextRenewalRetry 第一次 reader 请求返回 -2012，
// 客户端应自动 renewal + 重试成功。
func TestFetchChapterTextRenewalRetry(t *testing.T) {
	xhtml := `<html><body><p>重试后的正文</p></body></html>`
	var readerCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/web/reader/", func(w http.ResponseWriter, r *http.Request) {
		readerCalls++
		if readerCalls == 1 {
			w.Write([]byte(`{"errCode":-2012,"errMsg":"登录超时"}`))
			return
		}
		fmt.Fprintf(w, `<script>window.__INITIAL_STATE__ = {"reader":{"bookInfo":{"bookId":"43208843"},"psvts":"p1","token":""}};(function(){})()</script>`)
	})
	mux.HandleFunc("/web/login/renewal", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "wr_skey=newkey; Domain=.weread.qq.com; Path=/")
		w.Write([]byte(`{"succ":1}`))
	})
	mux.HandleFunc("/web/book/chapter/e_0", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(shardWithMD5(encodeForTest(xhtml))))
	})
	mux.HandleFunc("/web/book/chapter/e_1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(shardWithMD5("")))
	})
	mux.HandleFunc("/web/book/chapter/e_3", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(shardWithMD5("")))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	saved := false
	c.SetSaveHook(func() { saved = true })
	text, _, err := c.FetchChapterText("43208843", "1002")
	if err != nil {
		t.Fatal(err)
	}
	if text != "重试后的正文" {
		t.Errorf("text = %q", text)
	}
	if !saved {
		t.Error("renewal 未触发 saveHook")
	}
	if got := c.Jar.Snapshot()["wr_skey"]; got != "newkey" {
		t.Errorf("wr_skey = %q, want newkey", got)
	}
	if readerCalls != 2 {
		t.Errorf("readerCalls = %d, want 2", readerCalls)
	}
}

func TestFetchChapterTextRejectsInvalidOrEmptyPayload(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{name: "invalid base64", payload: "A!!!"},
		{name: "empty chapter", payload: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/web/reader/", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `<script>window.__INITIAL_STATE__ = {"reader":{"bookInfo":{"bookId":"43208843"},"psvts":"p1"}};(function(){})()</script>`)
			})
			for _, endpoint := range []string{"/web/book/chapter/e_0", "/web/book/chapter/e_1", "/web/book/chapter/e_3"} {
				mux.HandleFunc(endpoint, func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(shardWithMD5(tc.payload)))
				})
			}
			srv := httptest.NewServer(mux)
			defer srv.Close()
			c := newTestClient(srv)

			text, _, err := c.FetchChapterText("43208843", "1002")
			if err == nil {
				t.Fatalf("FetchChapterText() = %q, nil; want error", text)
			}
		})
	}
}

func TestCheckedBodyRejectsMalformedShortShard(t *testing.T) {
	if _, err := checkedBody("too-short"); err == nil {
		t.Fatal("checkedBody() accepted malformed short shard")
	}
	if body, err := checkedBody(shardWithMD5("")); err != nil || body != "" {
		t.Fatalf("valid empty shard = %q, %v", body, err)
	}
}
