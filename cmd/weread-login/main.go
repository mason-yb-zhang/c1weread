// PC 扫码登录工具：跑 QR 登录状态机，把凭证写到 JSON。
// 用法：weread-login-win.exe -out creds.json
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"c1weread/weread"
	"github.com/skip2/go-qrcode"
)

func main() {
	out := flag.String("out", "creds.json", "凭证输出路径")
	flag.Parse()

	jar := weread.NewJar()
	client := weread.NewClient(jar, nil)

	otpCallback := func(prompt string) (string, error) {
		fmt.Println(prompt)
		fmt.Println("（把手机上显示的 4 位验证码写进本目录的 otp.txt 文件即可，例如: echo 1234 > otp.txt）")
		deadline := time.Now().Add(150 * time.Second)
		for time.Now().Before(deadline) {
			if b, err := os.ReadFile("otp.txt"); err == nil {
				s := strings.TrimSpace(string(b))
				if len(s) >= 4 {
					_ = os.Remove("otp.txt")
					fmt.Println("已读取验证码:", s[:4])
					return s[:4], nil
				}
			}
			time.Sleep(time.Second)
		}
		return "", fmt.Errorf("等待 otp.txt 超时")
	}

	// 二维码服务端有效期短，过期自动换新重扫（最多 4 轮）。
	deadline := time.Now().Add(600 * time.Second)
	var flow *weread.LoginFlow
	var loginResult map[string]any
	for round := 1; round <= 4; round++ {
		if time.Now().After(deadline) {
			fatal("登录失败: 超过总时限")
		}
		f, err := weread.StartLogin(client, "")
		if err != nil {
			fatal("启动登录失败: ", err)
		}
		flow = f
		pngPath := fmt.Sprintf("weread-qr-%d.png", round)
		if err := qrcode.WriteFile(f.ConfirmURL(), qrcode.Medium, 320, pngPath); err == nil {
			fmt.Printf("=== 第 %d 轮二维码: %s（请在一分钟内扫码并在手机上确认）===\n", round, pngPath)
		}
		loginResult, err = f.WaitForLogin(70*time.Second, otpCallback)
		if err == weread.ErrLoginStopped {
			fmt.Println("本轮二维码已过期，自动换新的…")
			continue
		}
		if err != nil {
			fatal("登录失败: ", err)
		}
		break
	}
	if loginResult == nil {
		fatal("登录失败: 多轮二维码均未被确认")
	}

	fmt.Println("登录成功，正在取账号信息…")
	result, err := flow.Finish(loginResult)
	if err != nil {
		fatal("获取凭证失败: ", err)
	}
	if err := weread.SaveCredentials(*out, result.Creds); err != nil {
		fatal("写凭证失败: ", err)
	}
	fmt.Printf("完成。账号: %s\n", result.Account)
	fmt.Printf("凭证已写入: %s\n", *out)
	fmt.Println()
	fmt.Println("部署到设备：")
	fmt.Println("  adb push " + *out + " /storage/c1/local-apps/apps/weread/data/creds.json")
	fmt.Println("然后在设备 local-apps 列表按 R 刷新并启动微信读书。")
}

func fatal(parts ...any) {
	fmt.Fprintln(os.Stderr, parts...)
	os.Exit(1)
}
