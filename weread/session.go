// 会话续期：/web/login/renewal，10 分钟冷却，登录态错误自动 renewal+重试一次。
package weread

import (
	"encoding/json"
	"sync"
	"time"
)

const renewCooldown = 10 * time.Minute

// Renew 调用续期接口；仅 succ==1 才视为成功并触发持久化回调。
func (c *Client) Renew() error {
	raw, err := c.postJSON("/web/login/renewal", map[string]any{
		"rq": "%2Fweb%2Fbook%2Fread",
		"ql": false,
	})
	if err != nil {
		return err
	}
	var result struct {
		Succ int `json:"succ"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if result.Succ != 1 {
		return &APIError{Code: -1, Msg: "renewal 未返回 succ=1"}
	}
	c.mu.Lock()
	c.lastRenew = time.Now()
	c.mu.Unlock()
	if c.saveHook != nil {
		c.saveHook()
	}
	return nil
}

// renewIfCooldown 冷却期外才续期（用于请求前预热）。
func (c *Client) renewIfCooldown() error {
	c.mu.Lock()
	fresh := time.Since(c.lastRenew) < renewCooldown
	c.mu.Unlock()
	if fresh {
		return nil
	}
	return c.Renew()
}

// WithRenewal 执行 fn；遇登录超时（-2012）或空响应时自动 renewal 并重试一次。
func (c *Client) WithRenewal(fn func() error) error {
	err := fn()
	if err == nil {
		return nil
	}
	if !IsLoginExpired(err) {
		return err
	}
	if renewErr := c.Renew(); renewErr != nil {
		return err
	}
	return fn()
}

// renewalMutex 保证设备端并发请求只有一个在 renewal。
var renewalMutex sync.Mutex

// WithRenewalLocked 串行版的 WithRenewal（防并发重复续期）。
func (c *Client) WithRenewalLocked(fn func() error) error {
	renewalMutex.Lock()
	defer renewalMutex.Unlock()
	return c.WithRenewal(fn)
}
