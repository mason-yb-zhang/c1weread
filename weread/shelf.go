// 官方 agent gateway：书架 / 搜索 / 进度。
package weread

import (
	"encoding/json"
	"fmt"
)

const (
	gatewayURL      = "https://i.weread.qq.com/api/agent/gateway"
	skillVersion    = "1.0.5"
	shelfAPIName    = "/shelf/sync"
	searchAPIName   = "/store/search"
	progressAPIName = "/book/getprogress"
)

// ShelfBook 书架条目；Album=true 表示有声书（不支持阅读）。
type ShelfBook struct {
	BookID string
	Title  string
	Author string
	Cover  string
	Album  bool
}

// gatewayCall 调用官方 gateway，业务参数顶层平铺。
func (c *Client) gatewayCall(apiName string, params map[string]any) (json.RawMessage, error) {
	body := map[string]any{
		"api_name":      apiName,
		"skill_version": skillVersion,
	}
	for k, v := range params {
		body[k] = v
	}
	raw, err := c.postJSONGateway(body)
	if err != nil {
		return nil, err
	}
	// upgrade_info 处理：需要升级时返回明确错误
	var probe struct {
		UpgradeInfo any `json:"upgrade_info"`
	}
	if json.Unmarshal(raw, &probe) == nil && probe.UpgradeInfo != nil {
		return nil, fmt.Errorf("weread: gateway 要求升级 skill_version: %v", probe.UpgradeInfo)
	}
	return raw, nil
}

// postJSONGateway 带 Authorization 头的 gateway 请求。
func (c *Client) postJSONGateway(body map[string]any) (json.RawMessage, error) {
	raw, err := c.request("POST", gatewayURL, body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

// Shelf 拉取书架（/shelf/sync）。
func (c *Client) Shelf() ([]ShelfBook, error) {
	raw, err := c.gatewayCall(shelfAPIName, nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Books []struct {
			BookID string `json:"bookId"`
			Title  string `json:"title"`
			Author string `json:"author"`
			Cover  string `json:"cover"`
		} `json:"books"`
		Albums []struct {
			AlbumInfo struct {
				AlbumID    string `json:"albumId"`
				Name       string `json:"name"`
				AuthorName string `json:"authorName"`
				Cover      string `json:"cover"`
			} `json:"albumInfo"`
		} `json:"albums"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	books := make([]ShelfBook, 0, len(payload.Books)+len(payload.Albums))
	for _, b := range payload.Books {
		books = append(books, ShelfBook{
			BookID: b.BookID, Title: b.Title, Author: b.Author, Cover: b.Cover,
		})
	}
	for _, a := range payload.Albums {
		books = append(books, ShelfBook{
			BookID: a.AlbumInfo.AlbumID,
			Title:  a.AlbumInfo.Name,
			Author: a.AlbumInfo.AuthorName,
			Cover:  a.AlbumInfo.Cover,
			Album:  true,
		})
	}
	return books, nil
}

// SearchResult 搜索结果条目。
type SearchResult struct {
	BookID string
	Title  string
	Author string
	Cover  string
}

// Search 官方搜索（/store/search，scope=10 电子书）。
func (c *Client) Search(keyword string) ([]SearchResult, error) {
	raw, err := c.gatewayCall(searchAPIName, map[string]any{
		"keyword": keyword,
		"scope":   10,
	})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Results []struct {
			Books []struct {
				BookInfo struct {
					BookID string `json:"bookId"`
					Title  string `json:"title"`
					Author string `json:"author"`
					Cover  string `json:"cover"`
				} `json:"bookInfo"`
			} `json:"books"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var out []SearchResult
	for _, r := range payload.Results {
		for _, b := range r.Books {
			out = append(out, SearchResult{
				BookID: b.BookInfo.BookID,
				Title:  b.BookInfo.Title,
				Author: b.BookInfo.Author,
				Cover:  b.BookInfo.Cover,
			})
		}
	}
	return out, nil
}

// GetProgress 读取一本书的服务端进度（/book/getprogress）。
func (c *Client) GetProgress(bookID string) (progress int, err error) {
	raw, err := c.gatewayCall(progressAPIName, map[string]any{"bookId": bookID})
	if err != nil {
		return 0, err
	}
	var payload struct {
		Book struct {
			Progress int `json:"progress"`
		} `json:"book"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, err
	}
	return payload.Book.Progress, nil
}
