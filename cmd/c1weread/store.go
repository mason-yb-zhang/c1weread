// 本地存储：数据目录、凭证、书籍 manifest、阅读进度。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"c1weread/weread"
)

const defaultDataDir = "/storage/c1/local-apps/apps/weread/data"

// dataDir 返回数据目录（C1WEREAD_DATA 可覆盖，便于 PC 预览测试）。
func dataDir() string {
	if dir := os.Getenv("C1WEREAD_DATA"); dir != "" {
		return dir
	}
	return defaultDataDir
}

func ensureDataDir() error {
	return os.MkdirAll(dataDir(), 0o755)
}

func credsPath() string    { return filepath.Join(dataDir(), "creds.json") }
func progressPath() string { return filepath.Join(dataDir(), "progress.json") }

// loadCreds 读凭证；文件缺失返回 (nil, nil)。
func loadCreds() (*weread.Credentials, error) {
	return weread.LoadCredentials(credsPath())
}

// saveCreds 原子写凭证。
func saveCreds(creds *weread.Credentials) error {
	return weread.SaveCredentials(credsPath(), creds)
}

// chapterMeta manifest 里的章节条目。
type chapterMeta struct {
	UID   string `json:"uid"`
	Idx   int    `json:"idx"`
	Title string `json:"title"`
}

// manifest 一本书的本地元数据。
type manifest struct {
	BookID   string        `json:"bookId"`
	Title    string        `json:"title"`
	Author   string        `json:"author"`
	Chapters []chapterMeta `json:"chapters"`
}

func bookDir(bookID string) string { return filepath.Join(dataDir(), "books", bookID) }
func manifestPath(bookID string) string {
	return filepath.Join(bookDir(bookID), "manifest.json")
}

// chapterFile 章节正文文件路径。
func chapterFile(bookID string, ch chapterMeta) string {
	return filepath.Join(bookDir(bookID), fmt.Sprintf("%d_%s.txt", ch.Idx, ch.UID))
}

// loadManifest 读已下载书籍的 manifest；缺失返回 (nil, nil)。
func loadManifest(bookID string) (*manifest, error) {
	data, err := os.ReadFile(manifestPath(bookID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("解析 manifest: %w", err)
	}
	return &m, nil
}

// saveManifest 写 manifest（先建目录）。
func saveManifest(m *manifest) error {
	if err := os.MkdirAll(bookDir(m.BookID), 0o755); err != nil {
		return err
	}
	return atomicWrite(manifestPath(m.BookID), m)
}

// atomicWrite JSON 原子写。
func atomicWrite(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
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

// bookProgress 单本书的阅读进度。
type bookProgress struct {
	ChapterIdx int `json:"chapterIdx"`
	Page       int `json:"page"`
}

// loadProgress 读全部进度。
func loadProgress() (map[string]bookProgress, error) {
	data, err := os.ReadFile(progressPath())
	if os.IsNotExist(err) {
		return make(map[string]bookProgress), nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]bookProgress
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]bookProgress), nil
	}
	return m, nil
}

// saveProgress 原子写全部进度。
func saveProgress(p map[string]bookProgress) error {
	return atomicWrite(progressPath(), p)
}
