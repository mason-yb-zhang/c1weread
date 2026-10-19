// 微信读书 — C1-Slim 墨水屏客户端。
// 书架/目录/阅读三段式；凭证由 PC 工具 weread-login 生成后 adb push。
package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"c1device"
	"c1weread/weread"
)

//go:embed assets/pkg-font.bin
var fontData []byte

type app struct {
	face   *c1device.Face
	client *weread.Client
	creds  *weread.Credentials

	view   viewMode
	status string // 书架状态行
	errMsg string

	shelf             []weread.ShelfBook
	shelfSel          int
	shelfFirst        int
	currentBook       *manifest
	chapters          []chapterMeta
	catSel            int
	catFirst          int
	downloadTotal     int
	downloadDone      int
	downloadCancelled bool
	dlDone            chan error // 后台下载完成信号（nil=完成/取消；非 nil=失败原因）
	readerLines       []string // 当前章全部折行
	readerPages       int
	readerPage        int
	readerTitle       string
	progress          map[string]bookProgress
}

func main() {
	debug.SetMemoryLimit(12 << 20)
	debug.SetGCPercent(50)

	face, err := c1device.NewBitmapFace(fontData, 16)
	if err != nil {
		fmt.Fprintln(os.Stderr, "c1weread: font:", err)
		os.Exit(1)
	}
	defer face.Close()

	if err := ensureDataDir(); err != nil {
		fmt.Fprintln(os.Stderr, "c1weread: data dir:", err)
		os.Exit(1)
	}

	creds, err := loadCreds()
	if err != nil {
		fmt.Fprintln(os.Stderr, "c1weread: creds:", err)
	}
	progress, err := loadProgress()
	if err != nil {
		fmt.Fprintln(os.Stderr, "c1weread: progress:", err)
		progress = make(map[string]bookProgress)
	}

	a := &app{face: face, creds: creds, progress: progress}
	if creds == nil || creds.Cookies["wr_vid"] == "" {
		a.view = viewGuide
	} else {
		a.view = viewShelf
		a.setupClient()
	}

	platform, err := c1device.OpenPlatform()
	if err != nil {
		fmt.Fprintln(os.Stderr, "c1weread: open platform:", err)
		os.Exit(1)
	}
	defer platform.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	drawCount := 0
	dirty := true
	draw := func() {
		full := drawCount%12 == 0
		drawCount++
		_ = platform.Draw(a.render(), full)
		dirty = false
	}
	// 首帧全刷
	_ = platform.Draw(a.render(), true)

	// 书架异步同步
	if a.view == viewShelf {
		go a.syncShelf()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-platform.Events():
			if !ok {
				return
			}
			if ev.Repeat && ev.Key != c1device.KeyUp && ev.Key != c1device.KeyDown {
				continue
			}
			if a.handleEvent(ev, cancel) {
				return
			}
			dirty = true
		case dlErr := <-a.dlDone:
			// 后台下载结束：统一在主 goroutine 里切换视图
			a.dlDone = nil
			if dlErr != nil {
				a.fail(dlErr.Error())
			} else if a.downloadCancelled {
				a.view = viewShelf
			} else {
				a.enterCatalog()
			}
			dirty = true
		case <-time.After(300 * time.Millisecond):
			// 周期刷新：下载进度条依赖这里
			dirty = true
		}
		if dirty {
			draw()
		}
	}
}

// setupClient 用凭证初始化 weread 客户端；renewal 落盘。
func (a *app) setupClient() {
	jar := weread.NewJar()
	a.creds.FillJar(jar)
	a.client = weread.NewClient(jar, nil)
	a.client.APIKey = a.creds.APIKey
	a.client.SetSaveHook(func() {
		creds := a.creds
		creds.CollectJar(jar)
		if err := saveCreds(creds); err != nil {
			fmt.Fprintln(os.Stderr, "c1weread: save creds:", err)
		}
	})
}

// syncShelf 后台同步书架。
func (a *app) syncShelf() {
	a.status = "同步中…"
	books, err := a.client.Shelf()
	if err != nil {
		a.status = "同步失败（OK 重试）"
		return
	}
	a.shelf = books
	a.status = ""
}

// handleEvent 分发按键；返回 true 表示退出。
func (a *app) handleEvent(ev c1device.Event, cancel context.CancelFunc) bool {
	switch a.view {
	case viewGuide:
		if ev.Key == c1device.KeyBack || ev.Key == c1device.KeyOK {
			cancel()
			return true
		}
	case viewShelf:
		return a.handleShelf(ev, cancel)
	case viewCatalog:
		return a.handleCatalog(ev)
	case viewReader:
		return a.handleReader(ev)
	case viewError:
		if ev.Key == c1device.KeyLeft || ev.Key == c1device.KeyBack || ev.Key == c1device.KeyOK {
			a.view = viewShelf
		}
	case viewDownload:
		if ev.Key == c1device.KeyBack {
			a.downloadCancelled = true
		}
	}
	return false
}

// handleShelf 书架按键。
func (a *app) handleShelf(ev c1device.Event, cancel context.CancelFunc) bool {
	n := len(a.shelf)
	switch ev.Key {
	case c1device.KeyUp:
		if n > 0 && a.shelfSel > 0 {
			a.shelfSel--
			if a.shelfSel < a.shelfFirst {
				a.shelfFirst = a.shelfSel
			}
		}
	case c1device.KeyDown:
		if n > 0 && a.shelfSel < n-1 {
			a.shelfSel++
			if a.shelfSel >= a.shelfFirst+listRows {
				a.shelfFirst = a.shelfSel - listRows + 1
			}
		}
	case c1device.KeyOK:
		if a.status == "同步失败（OK 重试）" {
			a.status = ""
			go a.syncShelf()
			return false
		}
		if n == 0 || a.shelf[a.shelfSel].Album {
			a.status = "有声书不支持阅读"
			return false
		}
		a.openBook(a.shelf[a.shelfSel])
	case c1device.KeyBack:
		cancel()
		return true
	}
	return false
}

// openBook 打开一本书：章节齐全直接进目录，否则先补全下载（后台 goroutine，主循环渲染进度）。
func (a *app) openBook(book weread.ShelfBook) {
	m, err := loadManifest(book.BookID)
	if err != nil {
		a.fail(fmt.Sprintf("读取本地书籍失败: %v", err))
		return
	}
	if m == nil {
		m = &manifest{BookID: book.BookID, Title: book.Title, Author: book.Author}
	}
	a.currentBook = m
	a.chapters = m.Chapters
	a.view = viewDownload
	a.downloadCancelled = false
	a.dlDone = make(chan error, 1)
	go a.downloadBook(m)
}

// missingChapters 统计未落盘的章节数。
func (a *app) missingChapters() int {
	missing := 0
	for _, ch := range a.currentBook.Chapters {
		if _, err := chapterText(a.currentBook, ch); err != nil {
			missing++
		}
	}
	return missing
}

// downloadBook 后台下载（或补全）全部章节；每章间隔 200ms。
// 结果经 a.dlDone 汇报：nil=完成或用户取消（看 a.downloadCancelled），非 nil=失败原因。
// 不在本函数里直接改视图，视图切换由主循环的 dlDone 分支统一处理。
func (a *app) downloadBook(m *manifest) {
	// 拉章节表
	if len(m.Chapters) == 0 {
		infos, err := a.fetchInfosWithRetry(m.BookID)
		if err != nil {
			a.dlDone <- fmt.Errorf("获取目录失败: %w", err)
			return
		}
		for _, info := range infos {
			m.Chapters = append(m.Chapters, chapterMeta{UID: info.ChapterUID, Idx: info.ChapterIdx, Title: info.Title})
		}
		if err := saveManifest(m); err != nil {
			a.dlDone <- fmt.Errorf("写 manifest 失败: %w", err)
			return
		}
		a.currentBook = m
		a.chapters = m.Chapters
	}
	a.downloadTotal = len(m.Chapters)
	a.downloadDone = 0
	for _, ch := range m.Chapters {
		if a.downloadCancelled {
			a.dlDone <- nil
			return
		}
		if _, err := chapterText(m, ch); err == nil {
			a.downloadDone++
			continue
		}
		text, _, err := a.fetchChapterWithRetry(m.BookID, ch.UID)
		if err != nil {
			a.dlDone <- fmt.Errorf("下载章节「%s」失败: %w", ch.Title, err)
			return
		}
		if err := writeChapterText(m, ch, text); err != nil {
			a.dlDone <- fmt.Errorf("保存章节失败: %w", err)
			return
		}
		a.downloadDone++
		time.Sleep(200 * time.Millisecond)
	}
	a.dlDone <- nil
}

// fetchInfosWithRetry 带 renewal 重试的目录拉取。
func (a *app) fetchInfosWithRetry(bookID string) ([]weread.ChapterInfo, error) {
	var infos []weread.ChapterInfo
	err := a.client.WithRenewalLocked(func() error {
		var err error
		infos, err = a.client.FetchChapterInfos(bookID)
		return err
	})
	return infos, err
}

// fetchChapterWithRetry 取章节正文；renewal 与重试由 FetchChapterText 内部完成，
// 这里不能再包 WithRenewalLocked（它会与 FetchChapterText 内部的锁自死锁）。
func (a *app) fetchChapterWithRetry(bookID, uid string) (string, string, error) {
	return a.client.FetchChapterText(bookID, uid)
}

// enterCatalog 进入目录视图并恢复进度。
func (a *app) enterCatalog() {
	a.view = viewCatalog
	a.catSel, a.catFirst = 0, 0
	if p, ok := a.progress[a.currentBook.BookID]; ok && len(a.chapters) > 0 {
		for i, ch := range a.chapters {
			if ch.Idx == p.ChapterIdx {
				a.catSel = i
				if a.catSel >= a.catFirst+listRows {
					a.catFirst = a.catSel - listRows + 1
				}
				break
			}
		}
	}
}

// handleCatalog 目录按键。
func (a *app) handleCatalog(ev c1device.Event) bool {
	n := len(a.chapters)
	switch ev.Key {
	case c1device.KeyUp:
		if n > 0 && a.catSel > 0 {
			a.catSel--
			if a.catSel < a.catFirst {
				a.catFirst = a.catSel
			}
		}
	case c1device.KeyDown:
		if n > 0 && a.catSel < n-1 {
			a.catSel++
			if a.catSel >= a.catFirst+listRows {
				a.catFirst = a.catSel - listRows + 1
			}
		}
	case c1device.KeyOK:
		if n > 0 {
			a.openChapter(a.catSel, 0)
		}
	case c1device.KeyLeft, c1device.KeyBack:
		a.view = viewShelf
	}
	return false
}

// openChapter 打开指定章节；缺章先补下载。page 为重进恢复的页码。
func (a *app) openChapter(idx, page int) {
	ch := a.chapters[idx]
	text, err := chapterText(a.currentBook, ch)
	if err != nil {
		// 按需补章
		fetched, _, ferr := a.fetchChapterWithRetry(a.currentBook.BookID, ch.UID)
		if ferr != nil {
			a.fail(fmt.Sprintf("章节「%s」下载失败: %v", ch.Title, ferr))
			return
		}
		if werr := writeChapterText(a.currentBook, ch, fetched); werr != nil {
			a.fail(fmt.Sprintf("保存章节失败: %v", werr))
			return
		}
		text = fetched
	}
	a.readerTitle = a.currentBook.Title + "·" + ch.Title
	a.readerLines = wrapText(text, textWidthPx)
	a.readerPages = (len(a.readerLines) + readerRows - 1) / readerRows
	if a.readerPages == 0 {
		a.readerPages = 1
	}
	a.readerPage = clamp(page, 0, a.readerPages-1)
	a.view = viewReader
}

// handleReader 阅读器按键。
func (a *app) handleReader(ev c1device.Event) bool {
	save := func(chapterIdx int) {
		a.progress[a.currentBook.BookID] = bookProgress{ChapterIdx: chapterIdx, Page: a.readerPage}
		if err := saveProgress(a.progress); err != nil {
			fmt.Fprintln(os.Stderr, "c1weread: save progress:", err)
		}
	}
	chapterIdx := a.chapters[a.catSel].Idx
	switch ev.Key {
	case c1device.KeyUp:
		if a.readerPage > 0 {
			a.readerPage--
			save(chapterIdx)
		} else if a.catSel > 0 {
			a.openChapter(a.catSel-1, lastPage(a, a.catSel-1))
		}
	case c1device.KeyDown:
		if a.readerPage < a.readerPages-1 {
			a.readerPage++
			save(chapterIdx)
		} else if a.catSel < len(a.chapters)-1 {
			a.openChapter(a.catSel+1, 0)
		}
	case c1device.KeyLeft, c1device.KeyBack:
		save(chapterIdx)
		a.view = viewCatalog
	}
	return false
}

// lastPage 计算某章最后一页（用于上翻跨章）。
func lastPage(a *app, idx int) int {
	ch := a.chapters[idx]
	text, err := chapterText(a.currentBook, ch)
	if err != nil {
		return 0
	}
	lines := wrapText(text, textWidthPx)
	pages := (len(lines) + readerRows - 1) / readerRows
	if pages <= 0 {
		return 0
	}
	return pages - 1
}

// fail 进入错误屏。
func (a *app) fail(msg string) {
	a.errMsg = msg
	a.view = viewError
}

// writeChapterText 章节正文落盘（目录保证存在）。
func writeChapterText(m *manifest, ch chapterMeta, text string) error {
	if err := os.MkdirAll(bookDir(m.BookID), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(bookDir(m.BookID), ".ch-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, chapterFile(m.BookID, ch))
}

// wrapText 把正文按屏宽折行：ASCII 半宽 8px，其他 16px。
func wrapText(text string, widthPx int) []string {
	var lines []string
	for _, raw := range splitLines(text) {
		if raw == "" {
			continue
		}
		current := ""
		w := 0
		for _, r := range raw {
			rw := runeAdvance(r)
			if w+rw > widthPx && current != "" {
				lines = append(lines, current)
				current = ""
				w = 0
			}
			current += string(r)
			w += rw
		}
		if current != "" {
			lines = append(lines, current)
		}
	}
	return lines
}

// splitLines 保留原文换行（TXT 的全角空格缩进不动）。
func splitLines(text string) []string {
	return strings.Split(text, "\n")
}

func runeAdvance(r rune) int {
	if r < 0x80 {
		return 8
	}
	return 16
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
