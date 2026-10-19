// 微信读书 Web 协议算法：_e 哈希、签名、sorted query。
// 严格对照参考实现 fetch_weread_epub.py 移植。
package weread

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// WereadE 对应参考实现的 weread_e：MD5 前缀 + 数字/非数字分支编码 + 尾校验。
func WereadE(value string) string {
	h := md5Hex(value)
	result := h[:3]

	var chunks []string
	typeFlag := "4"
	if isAllDigits(value) {
		typeFlag = "3"
		for i := 0; i < len(value); i += 9 {
			end := i + 9
			if end > len(value) {
				end = len(value)
			}
			var n uint64
			for _, c := range value[i:end] {
				n = n*10 + uint64(c-'0')
			}
			chunks = append(chunks, fmt.Sprintf("%x", n))
		}
	} else {
		var b strings.Builder
		for _, r := range value {
			b.WriteString(fmt.Sprintf("%x", r))
		}
		chunks = []string{b.String()}
	}

	result += typeFlag
	result += "2" + h[len(h)-2:]

	for i, chunk := range chunks {
		result += fmt.Sprintf("%02x", len(chunk)) + chunk
		if i < len(chunks)-1 {
			result += "g"
		}
	}

	if len(result) < 20 {
		result += h[:20-len(result)]
	}
	result += md5Hex(result)[:3]
	return result
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// WereadSign 对应 weread_sign：双累积器异或哈希，返回 hex(a+b) 不掩码。
func WereadSign(query string) string {
	const seed = 0x15051505
	var a, b uint32 = seed, seed
	runes := []rune(query)
	length := len(runes)
	for i := length - 1; i > 0; i -= 2 {
		a = (a ^ (uint32(runes[i]) << uint((length-i)%30))) & 0x7FFFFFFF
		b = (b ^ (uint32(runes[i-1]) << uint(i%30))) & 0x7FFFFFFF
	}
	sum := uint64(a) + uint64(b)
	return fmt.Sprintf("%x", sum)
}

// SortedQuery 按 key 字典序拼接 query，值按 JS 拼写（true/false/null），
// 百分号编码保留 [0-9A-Za-z-_.~]，其余 %XX 大写（等价 encodeURIComponent）。
func SortedQuery(params map[string]any) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+percentEncode(jsValue(params[key])))
	}
	return strings.Join(parts, "&")
}

// jsValue 把 Go 值转成 JavaScript JSON.stringify 拼写。
func jsValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case bool:
		if v {
			return "true"
		}
		return "false"
	case string:
		return v
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// percentEncode 自定义百分号编码：仅保留 [0-9A-Za-z-_.~]。
func percentEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
