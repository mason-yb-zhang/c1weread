// 凭证 JSON 存取：原子写（临时文件 + rename）。
package weread

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Credentials 设备/PC 共用的凭证集合。
type Credentials struct {
	Cookies map[string]string `json:"cookies"`
	APIKey  string            `json:"apikey,omitempty"`
	Account string            `json:"account,omitempty"`
}

// SaveCredentials 原子写凭证文件。
func SaveCredentials(path string, creds *Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".creds-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// LoadCredentials 读凭证文件；不存在返回 (nil, nil)。
func LoadCredentials(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("解析凭证 %s: %w", path, err)
	}
	return &creds, nil
}

// FillJar 把凭证 cookie 灌入 jar（跳过空值）。
func (creds *Credentials) FillJar(jar *Jar) {
	for name, value := range creds.Cookies {
		if value != "" {
			jar.Set(name, value)
		}
	}
}

// CollectJar 把 jar 当前快照回填进凭证。
func (creds *Credentials) CollectJar(jar *Jar) {
	creds.Cookies = jar.Snapshot()
}
