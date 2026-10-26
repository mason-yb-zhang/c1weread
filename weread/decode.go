// 章节分片解码：剥 MD5 前缀、拼接、换位还原、base64 解码。
// 严格对照参考实现 fetch_weread_epub.py 移植（不含 repair_utf8，Go 直接输出字节）。
package weread

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// checkedBody 剥掉 32 字符大写 MD5 前缀并校验；32 字符可表示合法空分片。
func checkedBody(responseText string) (string, error) {
	if responseText == "" {
		return "", nil
	}
	if len(responseText) < 32 {
		return "", fmt.Errorf("分片响应过短: %d 字节", len(responseText))
	}
	expected := responseText[:32]
	body := responseText[32:]
	actual := strings.ToUpper(md5Hex(body))
	if actual != expected {
		return "", fmt.Errorf("分片 MD5 校验失败: expected=%s actual=%s", expected, actual)
	}
	return body, nil
}

// decodeShards 拼接 e0+e1+e3（或 TXT 的 t0+t1）并解码。
func decodeShards(e0, e1, e3 string) (string, error) {
	parts := make([]string, 0, 3)
	for _, shard := range []string{e0, e1, e3} {
		if shard == "" {
			continue
		}
		body, err := checkedBody(shard)
		if err != nil {
			return "", err
		}
		parts = append(parts, body)
	}
	return decodeEncodedPayload(strings.Join(parts, ""))
}

// swapPositions 从尾部 n=min(4,ceil(len/10)) 个字符算出换位下标表。
func swapPositions(encoded string) []int {
	length := len(encoded)
	if length < 4 {
		return nil
	}
	if length < 11 {
		return []int{0, 2}
	}

	n := min(4, (length+9)/10)
	tmp := make([]byte, 0, n*12)
	// 尾 n 个字符的二进制串按 4 进制解释成十进制数字串
	for i := length - 1; i >= length-n; i-- {
		bin := fmt.Sprintf("%b", encoded[i])
		// int(bin, 4)：把二进制字符串当作四进制数解析（只含 0/1）
		val := 0
		for j := 0; j < len(bin); j++ {
			val = val*4 + int(bin[j]-'0')
		}
		tmp = append(tmp, []byte(fmt.Sprintf("%d", val))...)
	}

	m := length - n - 2
	step := len(fmt.Sprintf("%d", m))
	result := make([]int, 0, 10)
	for i := 0; len(result) < 10 && i+step < len(tmp); i += step {
		result = append(result, modParse(tmp[i:i+step])%m)
		result = append(result, modParse(tmp[i+1:i+1+step])%m)
	}
	return result
}

// modParse 解析十进制数字串片段（来自 tmp，必为数字）。
func modParse(s []byte) int {
	val := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			break
		}
		val = val*10 + int(s[i]-'0')
	}
	return val
}

// reverseSwaps 按位置表成对交换相邻字符；每对先 k=1 再 k=0，倒序执行。
func reverseSwaps(encoded string, positions []int) string {
	chars := []byte(encoded)
	for i := len(positions) - 1; i > 0; i -= 2 {
		for k := 0; k <= 1; k++ {
			left := positions[i] + k
			right := positions[i-1] + k
			if left < len(chars) && right < len(chars) {
				chars[left], chars[right] = chars[right], chars[left]
			}
		}
	}
	return string(chars)
}

// decodeEncodedPayload 解码拼接后的负载：丢首字符 → 换位还原 → base64url → base64。
func decodeEncodedPayload(encodedPayload string) (string, error) {
	if encodedPayload == "" {
		return "", nil
	}
	encodedPayload = encodedPayload[1:]
	reordered := reverseSwaps(encodedPayload, swapPositions(encodedPayload))
	reordered = strings.ReplaceAll(reordered, "-", "+")
	reordered = strings.ReplaceAll(reordered, "_", "/")
	var b strings.Builder
	for i := 0; i < len(reordered); i++ {
		c := reordered[i]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '+' || c == '/' {
			b.WriteByte(c)
		}
	}
	b64 := b.String()
	b64 += strings.Repeat("=", (4-len(b64)%4)%4)
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("正文 Base64 解码失败: %w", err)
	}
	return string(data), nil
}

// shardWithMD5 仅供测试/工具使用：给负载加 32 字符大写 MD5 前缀。
func shardWithMD5(body string) string {
	sum := md5.Sum([]byte(body))
	return strings.ToUpper(hex.EncodeToString(sum[:])) + body
}
