// PC 扫码登录状态机（设备端 v1 不用）。
// 流程：预热 → getLoginUid → 轮询 getLoginInfo（可带 4 位验证码）→ 装 cookie →
// userInfo / apikeyGet 验证并取账号信息。
package weread

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// 登录逻辑码。
const (
	LogicNeedOTP      = "NEED_OTP"
	LogicOTPNotMatch  = "OTP_NOT_MATCH"
	LogicLoginTimeout = "LOGIN_TIMEOUT"
	LogicOTPExpired   = "OTP_EXPIRED"
)

// ErrLoginStopped 登录超时/验证码过期，需要重新扫码。
var ErrLoginStopped = errors.New("weread: 登录已超时，请重新扫码")

// OTPCallback 需要 4 位验证码时回调（NEED_OTP / OTP_NOT_MATCH）。
type OTPCallback func(prompt string) (string, error)

// LoginFlow 一次扫码登录会话。
type LoginFlow struct {
	client *Client
	uid    string
	fp     string
}

// LoginResult 登录产物。
type LoginResult struct {
	Creds   *Credentials
	Vid     string
	Skey    string
	Account string
}

// NewFingerprint 生成 wr_fp：4 字节 BigEndian uint32 的十进制串。
func NewFingerprint() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return strconv.FormatUint(uint64(binary.BigEndian.Uint32(b[:])), 10)
}

// StartLogin 预热并申请登录 uid；fp 传空则新生成。
func StartLogin(client *Client, fp string) (*LoginFlow, error) {
	if fp == "" {
		fp = NewFingerprint()
	}
	flow := &LoginFlow{client: client, fp: fp}
	client.Jar.Set("wr_fp", fp)
	// 预热
	if _, err := client.request("GET", "/r/weread-skills", nil); err != nil {
		return nil, fmt.Errorf("预热 weread-skills: %w", err)
	}
	raw, err := client.request("GET", "/api/auth/getLoginUid", nil)
	if err != nil {
		return nil, fmt.Errorf("getLoginUid: %w", err)
	}
	var resp struct {
		UID string `json:"uid"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.UID == "" {
		return nil, fmt.Errorf("getLoginUid 未返回 uid: %s", truncate(string(raw), 120))
	}
	flow.uid = resp.UID
	return flow, nil
}

// Fingerprint 返回本次登录的 wr_fp（需随凭证持久化）。
func (f *LoginFlow) Fingerprint() string { return f.fp }

// ConfirmURL 二维码内容。
func (f *LoginFlow) ConfirmURL() string {
	return "https://weread.qq.com/web/confirm?uid=" + percentEncode(f.uid)
}

// Poll 单次长轮询（70s 超时由 HTTP client 控制）。
func (f *LoginFlow) Poll(otp string) (map[string]any, error) {
	q := url.Values{}
	q.Set("uid", f.uid)
	q.Set("otp", otp)
	raw, err := f.client.request("GET", "/api/auth/getLoginInfo?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("getLoginInfo 响应异常: %w", err)
	}
	return result, nil
}

// WaitForLogin 轮询直到成功或失败；NEED_OTP 时经 otpCallback 要验证码。
// 总限由调用方控制（建议 300s，UI 侧间隔重试）。
func (f *LoginFlow) WaitForLogin(pollTimeout time.Duration, otpCallback OTPCallback) (map[string]any, error) {
	if pollTimeout > 0 {
		f.client.HTTP.Timeout = pollTimeout
	}
	result, err := f.Poll("")
	for err == nil {
		if succeed, _ := result["succeed"].(bool); succeed {
			return result, nil
		}
		logicCode, _ := result["logicCode"].(string)
		switch logicCode {
		case LogicNeedOTP, LogicOTPNotMatch:
			if otpCallback == nil {
				return nil, fmt.Errorf("weread: 需要手机 4 位验证码但无回调")
			}
			prompt := "输入手机上显示的 4 位验证码: "
			if logicCode == LogicOTPNotMatch {
				prompt = "验证码不匹配，重新输入: "
			}
			otp, cbErr := otpCallback(prompt)
			if cbErr != nil {
				return nil, cbErr
			}
			result, err = f.Poll(otp)
			continue
		case LogicLoginTimeout, LogicOTPExpired:
			return nil, ErrLoginStopped
		default:
			return nil, fmt.Errorf("weread: 未知登录逻辑码 %q", logicCode)
		}
	}
	return nil, err
}

// Finish 登录成功后组装凭证：cookie 集 + userInfo 账号名 + 官方 apikey。
func (f *LoginFlow) Finish(loginResult map[string]any) (*LoginResult, error) {
	f.client.HTTP.Timeout = 25 * time.Second // 恢复普通请求超时
	webLoginVid := stringField(loginResult, "webLoginVid")
	accessToken := stringField(loginResult, "accessToken")
	refreshToken := stringField(loginResult, "refreshToken")
	if webLoginVid == "" || accessToken == "" {
		// 有些账号的 getLoginInfo 响应不直接返回这两个字段，
		// 凭证也可能只经 Set-Cookie 下发到 jar，这里兜底。
		snap := f.client.Jar.Snapshot()
		if webLoginVid == "" {
			webLoginVid = snap["wr_vid"]
		}
		if accessToken == "" {
			accessToken = snap["wr_skey"]
		}
	}
	if webLoginVid == "" || accessToken == "" {
		keys := make([]string, 0, len(loginResult))
		for k := range loginResult {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("weread: 登录响应缺少 webLoginVid/accessToken（实际字段: %v）", keys)
	}
	jar := f.client.Jar
	jar.Set("wr_vid", webLoginVid)
	jar.Set("wr_skey", accessToken)
	jar.Set("wr_ql", "0")
	if refreshToken != "" {
		jar.Set("wr_rt", percentEncode(refreshToken))
	}

	account, err := f.fetchAccountName(webLoginVid, accessToken)
	if err != nil {
		return nil, err
	}
	apiKey, err := f.fetchAPIKey(webLoginVid, accessToken)
	if err != nil {
		return nil, err
	}
	creds := &Credentials{
		Cookies: jar.Snapshot(),
		APIKey:  apiKey,
		Account: account,
	}
	return &LoginResult{Creds: creds, Vid: webLoginVid, Skey: accessToken, Account: account}, nil
}

// fetchAccountName GET /api/userInfo?userVid=（X-Vid/X-Skey 头，401 时 0.5s 重试 ≤3）。
func (f *LoginFlow) fetchAccountName(vid, skey string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		name, err := f.userInfoOnce(vid, skey)
		if err == nil {
			return name, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func (f *LoginFlow) userInfoOnce(vid, skey string) (string, error) {
	req, err := http.NewRequest("GET", "https://weread.qq.com/api/userInfo?userVid="+percentEncode(vid), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "https://weread.qq.com/r/weread-skills")
	req.Header.Set("X-Vid", vid)
	req.Header.Set("X-Skey", skey)
	if cookie := f.client.Jar.Header(); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := f.client.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f.client.Jar.MergeSetCookies(resp.Header.Values("Set-Cookie"))
	if resp.StatusCode == http.StatusUnauthorized {
		return "", errors.New("weread: userInfo 401")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("weread: userInfo HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(ioLimit(resp)).Decode(&payload); err != nil {
		return "", err
	}
	return payload.Name, nil
}

// fetchAPIKey GET /api/skills/apikeyGet?only_show=1。
func (f *LoginFlow) fetchAPIKey(vid, skey string) (string, error) {
	req, err := http.NewRequest("GET", "https://weread.qq.com/api/skills/apikeyGet?only_show=1", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "https://weread.qq.com/r/weread-skills")
	req.Header.Set("X-Vid", vid)
	req.Header.Set("X-Skey", skey)
	if cookie := f.client.Jar.Header(); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := f.client.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("weread: apikeyGet HTTP %d", resp.StatusCode)
	}
	var payload struct {
		APIKey string `json:"apikey"`
	}
	if err := json.NewDecoder(ioLimit(resp)).Decode(&payload); err != nil {
		return "", err
	}
	if payload.APIKey == "" {
		return "", errors.New("weread: 未取到官方 apikey（请在 App: 我-设置-微信读书技能 开通后重试）")
	}
	return payload.APIKey, nil
}

func stringField(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		// JSON 数字（如 webLoginVid 的数字 uid）
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}
