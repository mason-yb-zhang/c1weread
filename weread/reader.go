// 正文链路：reader HTML → psvts → chapterInfos → 分片解码 → 纯文本。
package weread

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 章节内容格式。
const (
	FormatEPUB = "epub"
	FormatTXT  = "txt"
)

// ReaderState reader HTML 中提取的状态。
type ReaderState struct {
	BookID    string
	BookTitle string
	Author    string
	Psvts     string
	Token     string
}

var initialStateRe = regexp.MustCompile(`(?s)window\.__INITIAL_STATE__\s*=\s*(\{.*?\})\s*;\s*\(function`)

// fieldFallback 逐字段正则回退提取。
func fieldFallback(html, field string) string {
	re := regexp.MustCompile(fmt.Sprintf(`"%s"\s*:\s*"([^"]*)"`, regexp.QuoteMeta(field)))
	m := re.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	return m[1]
}

// FetchReaderState 取 /web/reader/{e(bookId)}[k{e(chapterUid)}] 并解析 __INITIAL_STATE__。
func (c *Client) FetchReaderState(bookID, chapterUID string) (*ReaderState, error) {
	state := &ReaderState{}
	err := c.WithRenewalLocked(func() error {
		path := "/web/reader/" + WereadE(bookID)
		if chapterUID != "" {
			path += "k" + WereadE(chapterUID)
		}
		htmlText, err := c.getText(path)
		if err != nil {
			return err
		}
		var parsed *ReaderState
		parsed, err = parseReaderHTML(htmlText, bookID)
		if err != nil {
			return err
		}
		state = parsed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return state, nil
}

// parseReaderHTML 从 reader HTML 解析状态；JSON 提取失败时逐字段正则回退。
func parseReaderHTML(htmlText, wantBookID string) (*ReaderState, error) {
	state := &ReaderState{}
	match := initialStateRe.FindStringSubmatch(htmlText)
	if match != nil {
		var initial struct {
			Reader struct {
				BookInfo struct {
					BookID string `json:"bookId"`
					Title  string `json:"title"`
					Author string `json:"author"`
				} `json:"bookInfo"`
				Psvts string `json:"psvts"`
				Token string `json:"token"`
			} `json:"reader"`
		}
		if err := json.Unmarshal([]byte(match[1]), &initial); err == nil {
			state.BookID = initial.Reader.BookInfo.BookID
			state.BookTitle = initial.Reader.BookInfo.Title
			state.Author = initial.Reader.BookInfo.Author
			state.Psvts = initial.Reader.Psvts
			state.Token = initial.Reader.Token
		}
	}
	// 逐字段回退补齐
	if state.BookID == "" {
		state.BookID = fieldFallback(htmlText, "bookId")
	}
	if state.BookTitle == "" {
		state.BookTitle = fieldFallback(htmlText, "title")
	}
	if state.Author == "" {
		state.Author = fieldFallback(htmlText, "author")
	}
	if state.Psvts == "" {
		state.Psvts = fieldFallback(htmlText, "psvts")
	}
	if state.Token == "" {
		state.Token = fieldFallback(htmlText, "token")
	}
	if state.Psvts == "" {
		return nil, fmt.Errorf("weread: reader HTML 缺少 psvts")
	}
	if state.BookID == "" {
		state.BookID = wantBookID
	}
	if state.BookTitle == "" {
		state.BookTitle = state.BookID
	}
	return state, nil
}

// ChapterInfo 单章元数据。ChapterUID 保留原始 JSON 拼写（字符串/数字均可）。
type ChapterInfo struct {
	ChapterUID string
	ChapterIdx int
	Title      string
	WordCount  int
}

// FetchChapterInfos POST /web/book/chapterInfos，规范化三种响应形状，
// 过滤 wordCount>0 且非"封面"。
func (c *Client) FetchChapterInfos(bookID string) ([]ChapterInfo, error) {
	raw, err := c.postJSON("/web/book/chapterInfos", map[string]any{"bookIds": []string{bookID}})
	if err != nil {
		return nil, err
	}
	chapters, err := normalizeChapterInfos(raw, bookID)
	if err != nil {
		return nil, err
	}
	out := make([]ChapterInfo, 0, len(chapters))
	for _, ch := range chapters {
		if ch.WordCount > 0 && ch.Title != "封面" {
			out = append(out, ch)
		}
	}
	return out, nil
}

// normalizeChapterInfos 对齐参考实现：data[] / books[] / 顶层 三种形状。
func normalizeChapterInfos(raw json.RawMessage, bookID string) ([]ChapterInfo, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var candidates []any
	switch v := payload.(type) {
	case []any:
		candidates = v
	case map[string]any:
		if data, ok := v["data"].([]any); ok {
			candidates = data
		} else if books, ok := v["books"].([]any); ok {
			candidates = books
		} else {
			candidates = []any{v}
		}
	default:
		return nil, fmt.Errorf("weread: chapterInfos 响应形状异常")
	}

	for _, item := range candidates {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		recordBookID := jsonStringField(record, "bookId")
		if recordBookID == "" {
			if book, ok := record["book"].(map[string]any); ok {
				recordBookID = jsonStringField(book, "bookId")
			}
		}
		if recordBookID != bookID {
			continue
		}
		var chapterList []any
		for _, key := range []string{"updated", "chapterInfos", "chapters"} {
			if list, ok := record[key].([]any); ok {
				chapterList = list
				break
			}
		}
		chapters := make([]ChapterInfo, 0, len(chapterList))
		for _, entry := range chapterList {
			chMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			uid := jsonStringField(chMap, "chapterUid")
			if uid == "" {
				continue
			}
			chapters = append(chapters, ChapterInfo{
				ChapterUID: uid,
				ChapterIdx: jsonIntField(chMap, "chapterIdx"),
				Title:      jsonStringField(chMap, "title"),
				WordCount:  jsonIntField(chMap, "wordCount"),
			})
		}
		return chapters, nil
	}
	return nil, fmt.Errorf("weread: chapterInfos 中未找到 bookId=%s", bookID)
}

// jsonStringField 读取字段的字符串拼写（数字字段转十进制字符串）。
func jsonStringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return t.String()
	}
	return ""
}

func jsonIntField(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}

// FetchChapterText 取一章正文并转成纯文本：EPUB 书解码 XHTML 后抽取文本，
// TXT 书解码原文并归一换行。format 返回 FormatEPUB / FormatTXT。
func (c *Client) FetchChapterText(bookID, chapterUID string) (text string, format string, err error) {
	err = c.WithRenewalLocked(func() error {
		reader, err := c.fetchReaderStateDirect(bookID, chapterUID)
		if err != nil {
			return err
		}
		format, err = c.bookFormat(bookID, reader, chapterUID)
		if err != nil {
			return err
		}
		var decoded string
		if format == FormatTXT {
			decoded, err = c.fetchTXTChapter(bookID, chapterUID, reader)
		} else {
			decoded, err = c.fetchEPUBChapter(bookID, chapterUID, reader)
		}
		if err != nil {
			return err
		}
		if format == FormatTXT {
			text = NormalizeTXTText(decoded)
		} else {
			text = HTMLToText(decoded)
		}
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("weread: 章节正文解码后为空")
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return text, format, nil
}

// fetchReaderStateDirect 不走 WithRenewal 的 reader state（避免套娃）。
func (c *Client) fetchReaderStateDirect(bookID, chapterUID string) (*ReaderState, error) {
	path := "/web/reader/" + WereadE(bookID)
	if chapterUID != "" {
		path += "k" + WereadE(chapterUID)
	}
	htmlText, err := c.getText(path)
	if err != nil {
		return nil, err
	}
	return parseReaderHTML(htmlText, bookID)
}

// makeContentParams 构造内容请求参数（不含 s）。
func makeContentParams(bookID, chapterUID, psvts string) map[string]any {
	var seed [8]byte
	_, _ = rand.Read(seed[:])
	r := int(binary.LittleEndian.Uint64(seed[:]) % 10000)
	ct := time.Now().Unix()
	pc := WereadE(strconv.FormatInt(ct, 10))
	if pc == psvts {
		ct++
		pc = WereadE(strconv.FormatInt(ct, 10))
	}
	return map[string]any{
		"b":           WereadE(bookID),
		"c":           WereadE(chapterUID),
		"r":           strconv.Itoa(r * r),
		"ct":          strconv.FormatInt(ct, 10),
		"ps":          psvts,
		"pc":          pc,
		"sc":          1,
		"prevChapter": false,
		"st":          0,
	}
}

// signedParams 加签名后的完整参数。
func signedParams(params map[string]any) map[string]any {
	params["s"] = WereadSign(SortedQuery(params))
	return params
}

// bookFormat 探测并缓存书的内容格式（txt/epub）。
func (c *Client) bookFormat(bookID string, reader *ReaderState, chapterUID string) (string, error) {
	c.mu.Lock()
	cached, ok := c.formats[bookID]
	c.mu.Unlock()
	if ok {
		return cached, nil
	}
	e0, err := c.shardPost(bookID, chapterUID, reader, "/web/book/chapter/e_0")
	if err != nil {
		return "", err
	}
	format := FormatEPUB
	if len(e0) > 0 && e0[0] == '{' && bookIDFieldRe.MatchString(e0) {
		format = FormatTXT
	}
	c.mu.Lock()
	c.formats[bookID] = format
	c.mu.Unlock()
	return format, nil
}

var bookIDFieldRe = regexp.MustCompile(`"bookId"`)

// shardPost 带签名的分片请求。
func (c *Client) shardPost(bookID, chapterUID string, reader *ReaderState, endpoint string) (string, error) {
	params := signedParams(makeContentParams(bookID, chapterUID, reader.Psvts))
	raw, err := c.postJSON(endpoint, params)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// fetchTXTChapter t_0 + t_1；任一分片失败都中止，避免保存残缺正文。
func (c *Client) fetchTXTChapter(bookID, chapterUID string, reader *ReaderState) (string, error) {
	t0, err := c.shardPost(bookID, chapterUID, reader, "/web/book/chapter/t_0")
	if err != nil {
		return "", err
	}
	t1, err := c.shardPost(bookID, chapterUID, reader, "/web/book/chapter/t_1")
	if err != nil {
		return "", fmt.Errorf("获取 TXT 分片 t_1: %w", err)
	}
	return decodeShards(t0, t1, "")
}

// fetchEPUBChapter e_0 + e_1 + e_3 解码 XHTML。
func (c *Client) fetchEPUBChapter(bookID, chapterUID string, reader *ReaderState) (string, error) {
	e0, err := c.shardPost(bookID, chapterUID, reader, "/web/book/chapter/e_0")
	if err != nil {
		return "", err
	}
	e1, err := c.shardPost(bookID, chapterUID, reader, "/web/book/chapter/e_1")
	if err != nil {
		return "", err
	}
	e3, err := c.shardPost(bookID, chapterUID, reader, "/web/book/chapter/e_3")
	if err != nil {
		return "", err
	}
	return decodeShards(e0, e1, e3)
}
