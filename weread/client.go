// 微信读书 Web 客户端：HTTP 传输、统一请求头、Cookie jar、错误解析。
package weread

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed assets/cacert.pem
var caCertPEM []byte

// UserAgent 固定桌面 UA（对齐参考实现）。
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 " +
	"Safari/537.36 Edg/135.0.0.0"

const baseURL = "https://weread.qq.com"

// maxBody 限制单响应读取量（设备内存约束）。
const maxBody = 8 << 20

// ErrEmptyResponse 对应服务端返回 {} 空对象。
var ErrEmptyResponse = errors.New("weread: 空响应 {}")

// APIError 微信读书业务错误（errCode/errMsg）。
type APIError struct {
	Code int
	Msg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("weread: errCode=%d errMsg=%s", e.Code, e.Msg)
}

// IsLoginExpired 判断是否为登录超时（-2012）或空响应等可 renewal 重试的错误。
func IsLoginExpired(err error) bool {
	if errors.Is(err, ErrEmptyResponse) {
		return true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == -2012
	}
	return false
}

// Jar 简单 Cookie jar：仅保留 wr_* 白名单与少量腾讯通用 cookie。
type Jar struct {
	mu      sync.Mutex
	cookies map[string]string
}

func NewJar() *Jar {
	return &Jar{cookies: make(map[string]string)}
}

// jarWhitelist 允许入库的 cookie 名（wr_fp、wr_vid、wr_skey、wr_rt 等 wr_ 前缀 + 通用项）。
var jarExtra = map[string]bool{
	"ptcz": true, "RK": true, "pgv_pvid": true,
}

func jarAllowed(name string) bool {
	return strings.HasPrefix(name, "wr_") || jarExtra[name]
}

// Set 直接写入 cookie（空值 = 删除）。
func (j *Jar) Set(name, value string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if value == "" {
		delete(j.cookies, name)
		return
	}
	j.cookies[name] = value
}

// MergeSetCookies 合并响应 Set-Cookie 头，按白名单过滤。
func (j *Jar) MergeSetCookies(setCookies []string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, line := range setCookies {
		name, value, ok := parseSetCookie(line)
		if !ok || !jarAllowed(name) {
			continue
		}
		if value == "" {
			delete(j.cookies, name)
		} else {
			j.cookies[name] = value
		}
	}
}

// parseSetCookie 只取 Set-Cookie 行的第一段 name=value（忽略属性）。
func parseSetCookie(line string) (string, string, bool) {
	end := len(line)
	for i := 0; i < len(line); i++ {
		if line[i] == ';' {
			end = i
			break
		}
	}
	pair := strings.TrimSpace(line[:end])
	eq := strings.IndexByte(pair, '=')
	if eq <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(pair[:eq]), strings.TrimSpace(pair[eq+1:]), true
}

// Header 构造请求 Cookie 头（按名排序，稳定输出）。
func (j *Jar) Header() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	names := make([]string, 0, len(j.cookies))
	for name := range j.cookies {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+j.cookies[name])
	}
	return strings.Join(parts, "; ")
}

// Snapshot 返回 jar 的浅拷贝。
func (j *Jar) Snapshot() map[string]string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make(map[string]string, len(j.cookies))
	for k, v := range j.cookies {
		out[k] = v
	}
	return out
}

// Load 批量导入 cookie（覆盖同名）。
func (j *Jar) Load(cookies map[string]string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for k, v := range cookies {
		if v == "" {
			delete(j.cookies, k)
		} else {
			j.cookies[k] = v
		}
	}
}

// Client 微信读书 Web 客户端。
type Client struct {
	HTTP    *http.Client
	Jar     *Jar
	BaseURL string // 可指向 httptest server
	APIKey  string // 官方 gateway 的 Bearer key

	mu        sync.Mutex
	formats   map[string]string // bookId → "txt"/"epub"，格式探测缓存
	lastRenew time.Time
	saveHook  func() // renewal 成功后持久化回调
}

// NewClient 构造客户端；caPEM 为 nil 时使用内嵌 cacert.pem。
func NewClient(jar *Jar, caPEM []byte) *Client {
	if len(caPEM) == 0 {
		caPEM = caCertPEM
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(caPEM)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}
	return &Client{
		HTTP:    &http.Client{Timeout: 25 * time.Second, Transport: transport},
		Jar:     jar,
		BaseURL: baseURL,
		formats: make(map[string]string),
	}
}

// SetSaveHook 设置 renewal 成功后的持久化回调。
func (c *Client) SetSaveHook(hook func()) {
	c.saveHook = hook
}

// request 发起统一请求：桌面 UA、JSON content-type、Origin/Referer、Cookie 头。
// 返回响应体；解析 errCode 与空响应。
func (c *Client) request(method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	url := path
	if !strings.HasPrefix(url, "http") {
		url = c.BaseURL + path
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://weread.qq.com")
	req.Header.Set("Referer", "https://weread.qq.com/")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	}
	if cookie := c.Jar.Header(); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	c.Jar.MergeSetCookies(resp.Header.Values("Set-Cookie"))
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("weread: HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "{}" {
		return data, ErrEmptyResponse
	}
	// 尝试解析 wr 错误码
	var probe struct {
		ErrCode int    `json:"errCode"`
		ErrMsg  string `json:"errMsg"`
	}
	if json.Unmarshal(data, &probe) == nil && probe.ErrCode != 0 {
		return data, &APIError{Code: probe.ErrCode, Msg: probe.ErrMsg}
	}
	return data, nil
}

// getText 发起 GET 并返回文本（reader HTML 用）。
func (c *Client) getText(path string) (string, error) {
	data, err := c.request("GET", path, nil)
	return string(data), err
}

// postJSON 发起 POST，返回解析后的 JSON。
func (c *Client) postJSON(path string, body any) (json.RawMessage, error) {
	data, err := c.request("POST", path, body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ioLimit 限制响应体读取量（设备内存约束）。
func ioLimit(resp *http.Response) io.Reader {
	return io.LimitReader(resp.Body, maxBody)
}
