// 微信读书 — C1-Slim 墨水屏客户端。
// 书架/目录/阅读三段式；凭证由 PC 工具 weread-login 生成后 adb push。
package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"

	"c1device"
	"c1weread/weread"
)

//go:embed assets/pkg-font.bin
var fontData []byte

type recentBook struct {
	BookID       string
	Title        string
	ChapterTitle string
	ChapterIdx   int
	Page         int
	Updated      int64
}

type shelfResult struct {
	books []weread.ShelfBook
	err   error
}

type downloadEvent struct {
	total     int
	done      int
	book      *manifest
	finished  bool
	cancelled bool
	err       error
}

type app struct {
	face     *c1device.Face // UI 字体（内嵌 Unifont 16px）
	bodyFace *c1device.Face // 正文字体
	fontData *mappedFont    // TTF 运行时字体数据（Face 借用其 typeface）
	client   *weread.Client
	creds    *weread.Credentials

	view    viewMode
	status  string // 书架状态行
	errMsg  string
	message string // 阅读器/设置等临时提示

	shelf       []weread.ShelfBook
	shelfSel    int
	shelfFirst  int
	shelfDone   chan shelfResult
	shelfOnline bool
	currentBook *manifest
	chapters    []chapterMeta
	catSel      int
	catFirst    int

	downloadTotal  int
	downloadDone   int
	downloadCancel context.CancelFunc
	dlEvents       chan downloadEvent

	pages       []Page // 当前章分页结果
	readerPage  int
	readerTitle string
	fullscreen  bool // 阅读器全屏模式（会话级，不持久化）
	settings    ReaderSettings
	settingsSel int
	progress    map[string]bookProgress

	recentBooks []recentBook
	recentSel   int
	recentFirst int

	// 全书百分比统计：bookPrefix[i] = chapters[i] 之前所有章节落盘字节合计；bookTotal = 全书总字节。
	bookPrefix []int64
	bookTotal  int64

	// 书架/最近阅读打开时若有进度，下载完成后直接恢复阅读
	resumeChapter int
	resumePage    int
}

// writeTrace 向数据目录的 c1weread.trace 追加一行（崩溃/信号诊断用，与启动器日志无关）。
func writeTrace(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(dataDir(), "c1weread.trace"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, format+"\n", args...)
	f.Sync()
}

func main() {
	debug.SetMemoryLimit(12 << 20)
	debug.SetGCPercent(50)

	// 崩溃/信号留痕见 writeTrace：无论什么启动方式都写数据目录里的 trace 文件，事后可查。
	// 顶层 recover：panic 不再无声闪退，留堆栈后退出。
	defer func() {
		if r := recover(); r != nil {
			writeTrace("panic: %v\n%s", r, debug.Stack())
			panic(r)
		}
	}()
	// SIGHUP 防护：C1ancher 系的子进程管理用 PDEATHSIG=SIGHUP，
	// Go 默认收到 HUP 即退出（表现为"运行一会就闪退"），这里直接忽略。
	signal.Ignore(syscall.SIGHUP)
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		writeTrace("signal %v pid=%d at %s", sig, os.Getpid(), time.Now().Format(time.RFC3339))
		os.Exit(0)
	}()

	face, err := c1device.NewBitmapFace(fontData, 16)
	if err != nil {
		fmt.Fprintln(os.Stderr, "c1weread: ui font:", err)
		os.Exit(1)
	}
	defer face.Close()

	if err := ensureDataDir(); err != nil {
		fmt.Fprintln(os.Stderr, "c1weread: data dir:", err)
		os.Exit(1)
	}

	settings, settingsErr := loadReaderSettings()
	bodyFace, fontData, err := newReaderFace(settings)
	if err != nil {
		if settingsErr == nil {
			fmt.Fprintf(os.Stderr, "c1weread: body font (%s): %v; fallback unifont\n", settings.Font, err)
		}
		settings = defaultReaderSettings()
		bodyFace, fontData, err = newReaderFace(settings)
		if err != nil {
			fmt.Fprintln(os.Stderr, "c1weread: fallback font:", err)
			os.Exit(1)
		}
	}
	defer bodyFace.Close()
	if fontData != nil {
		defer fontData.Close()
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

	localShelf := loadLocalShelf()
	a := &app{face: face, bodyFace: bodyFace, fontData: fontData, settings: settings, creds: creds, progress: progress, shelf: localShelf}
	if creds == nil || creds.Cookies["wr_vid"] == "" {
		if len(localShelf) == 0 {
			a.view = viewGuide
		} else {
			a.view = viewShelf
			a.status = "离线书库（未登录）"
		}
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

	// 书架异步同步；本地书库在同步前及断网时仍可使用。
	if a.view == viewShelf && a.client != nil {
		a.startShelfSync()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-platform.Events():
			if !ok {
				return
			}
			// 音量键映射为方向键（同 reader-lab）：音量+ = ↓ 下页/下移，音量- = ↑ 上页/上移。
			// 在 repeat 过滤之前 remap，长按音量键可连续翻页。
			switch ev.Key {
			case c1device.KeyVolumeUp:
				ev.Key = c1device.KeyDown
			case c1device.KeyVolumeDown:
				ev.Key = c1device.KeyUp
			}
			if ev.Repeat && ev.Key != c1device.KeyUp && ev.Key != c1device.KeyDown {
				continue
			}
			if a.handleEvent(ev, cancel) {
				return
			}
			dirty = true
		case result := <-a.shelfDone:
			// 网络结果只在主 goroutine 合并，避免渲染和按键读写竞争。
			a.shelfDone = nil
			if result.err != nil {
				a.shelfOnline = false
				a.status = "同步失败（OK 重试）"
				if len(a.shelf) > 0 {
					a.status = "同步失败（离线书架）"
				}
			} else {
				a.shelfOnline = true
				a.shelf = mergeShelfBooks(result.books, loadLocalShelf())
				a.status = ""
				if len(a.shelf) == 0 {
					a.shelfSel, a.shelfFirst = 0, 0
				} else {
					a.shelfSel = clamp(a.shelfSel, 0, len(a.shelf)-1)
					a.shelfFirst = clamp(a.shelfFirst, 0, a.shelfSel)
				}
			}
			dirty = true
		case ev := <-a.dlEvents:
			// 下载进度和最终状态同样只由主 goroutine 应用。
			if !ev.finished {
				a.downloadTotal, a.downloadDone = ev.total, ev.done
				dirty = true
				break
			}
			a.dlEvents = nil
			a.downloadCancel = nil
			if ev.book != nil {
				a.currentBook = ev.book
				a.chapters = ev.book.Chapters
				a.refreshBookSizes()
			}
			if ev.err != nil {
				a.fail(ev.err.Error())
			} else if ev.cancelled {
				a.view = viewShelf
			} else if a.resumeChapter >= 0 {
				idx := a.chapterIndexForIdx(a.resumeChapter)
				if idx >= 0 {
					a.openChapter(idx, a.resumePage)
				} else {
					a.enterCatalog()
				}
				a.resumeChapter = -1
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

// startShelfSync 启动后台请求；结果经 channel 交回主事件循环。
func (a *app) startShelfSync() {
	if a.client == nil || a.shelfDone != nil {
		return
	}
	a.status = "同步中…"
	results := make(chan shelfResult, 1)
	a.shelfDone = results
	client := a.client
	go func() {
		books, err := client.Shelf()
		results <- shelfResult{books: books, err: err}
	}()
}

// handleEvent 分发按键；返回 true 表示退出。
func (a *app) handleEvent(ev c1device.Event, cancel context.CancelFunc) bool {
	ev.Key = a.orientation().RemapKey(ev.Key)
	a.message = "" // 临时提示只存活到下一次按键（reader-lab 同款语义）
	switch a.view {
	case viewGuide:
		if ev.Key == c1device.KeyBack || ev.Key == c1device.KeyOK {
			cancel()
			return true
		}
	case viewShelf:
		return a.handleShelf(ev, cancel)
	case viewRecent:
		return a.handleRecent(ev)
	case viewCatalog:
		return a.handleCatalog(ev)
	case viewReader:
		return a.handleReader(ev)
	case viewSettings:
		return a.handleSettings(ev)
	case viewError:
		if ev.Key == c1device.KeyLeft || ev.Key == c1device.KeyBack || ev.Key == c1device.KeyOK {
			a.view = viewShelf
		}
	case viewDownload:
		if ev.Key == c1device.KeyBack && a.downloadCancel != nil {
			a.downloadCancel()
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
		if a.status == "同步失败（OK 重试）" && n == 0 {
			a.startShelfSync()
			return false
		}
		if n == 0 || a.shelf[a.shelfSel].Album {
			a.status = "有声书不支持阅读"
			return false
		}
		a.openBook(a.shelf[a.shelfSel])
	case c1device.KeyRune:
		if ev.Rune == 'r' || ev.Rune == 'R' {
			a.openRecent()
		}
	case c1device.KeyBack:
		cancel()
		return true
	}
	return false
}

// openBook 打开一本书：有进度且章节齐全则直达阅读器；否则下载（或补全）后恢复阅读或进目录。
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
	a.refreshBookSizes()

	p, hasProgress := a.progress[book.BookID]
	a.resumeChapter, a.resumePage = -1, 0
	if hasProgress {
		a.resumeChapter, a.resumePage = p.ChapterIdx, p.Page
	}

	missing := a.missingChapters()
	// 在线时即使缓存完整也经过目录刷新，连载书才能看到新章节；
	// 离线或尚未同步成功时优先直接恢复本地阅读。
	if hasProgress && missing == 0 && !a.shelfOnline && a.shelfDone == nil {
		idx := a.chapterIndexForIdx(p.ChapterIdx)
		if idx >= 0 {
			a.openChapter(idx, p.Page)
			return
		}
	}

	a.view = viewDownload
	a.downloadTotal, a.downloadDone = len(m.Chapters), 0
	downloadCtx, downloadCancel := context.WithCancel(context.Background())
	a.downloadCancel = downloadCancel
	events := make(chan downloadEvent, 1)
	a.dlEvents = events
	go a.downloadBook(downloadCtx, m, events)
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

// downloadBook 后台刷新目录并下载（或补全）全部章节；每章间隔 200ms。
// 后台只读写本书数据并通过 events 汇报，所有 app 状态由主事件循环更新。
func (a *app) downloadBook(ctx context.Context, m *manifest, events chan<- downloadEvent) {
	emit := func(ev downloadEvent) {
		ev.book = m
		events <- ev
	}
	fail := func(err error) {
		emit(downloadEvent{finished: true, err: err})
	}
	// panic 防护：下载在后台 goroutine 跑，崩了要留痕并回报失败而不是闪退。
	defer func() {
		if r := recover(); r != nil {
			writeTrace("download panic: %v\n%s", r, debug.Stack())
			fail(fmt.Errorf("下载内部错误: %v", r))
		}
	}()

	// 有网络客户端时每次打开都刷新目录；失败则保留旧目录作为离线回退。
	if a.client != nil {
		infos, err := a.fetchInfosWithRetry(m.BookID)
		if err != nil && len(m.Chapters) == 0 {
			fail(fmt.Errorf("获取目录失败: %w", err))
			return
		}
		if err == nil {
			if len(infos) == 0 {
				fail(fmt.Errorf("获取目录失败: 服务端返回空目录"))
				return
			}
			m.Chapters = mergeChapterInfos(infos)
			m.TotalBytes = 0
			if err := saveManifest(m); err != nil {
				fail(fmt.Errorf("写 manifest 失败: %w", err))
				return
			}
		}
	} else if len(m.Chapters) == 0 {
		fail(fmt.Errorf("未登录且没有本地目录"))
		return
	}

	done := 0
	emit(downloadEvent{total: len(m.Chapters), done: done})
	for _, ch := range m.Chapters {
		if ctx.Err() != nil {
			emit(downloadEvent{finished: true, cancelled: true})
			return
		}
		if _, err := chapterText(m, ch); err == nil {
			done++
			emit(downloadEvent{total: len(m.Chapters), done: done})
			continue
		}
		if a.client == nil {
			fail(fmt.Errorf("章节「%s」未下载，当前未登录", ch.Title))
			return
		}
		text, _, err := a.fetchChapterWithRetry(m.BookID, ch.UID)
		if err != nil {
			fail(fmt.Errorf("下载章节「%s」失败: %w", ch.Title, err))
			return
		}
		if err := writeChapterText(m, ch, text); err != nil {
			fail(fmt.Errorf("保存章节失败: %w", err))
			return
		}
		done++
		emit(downloadEvent{total: len(m.Chapters), done: done})
		select {
		case <-ctx.Done():
			emit(downloadEvent{finished: true, cancelled: true})
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	// 整本下载完成：记录全书总字节（顶栏百分比用），写失败不阻塞（可退回章节比例估算）。
	var total int64
	for _, ch := range m.Chapters {
		if fi, err := os.Stat(chapterFile(m.BookID, ch)); err == nil {
			total += fi.Size()
		}
	}
	if total > 0 {
		m.TotalBytes = total
		if err := saveManifest(m); err != nil {
			writeTrace("save manifest totalBytes: %v", err)
		}
	}
	emit(downloadEvent{total: len(m.Chapters), done: done, finished: true})
}

// mergeChapterInfos 用服务端目录替换章节顺序和元数据；UID/Idx 不变的缓存文件继续有效。
func mergeChapterInfos(infos []weread.ChapterInfo) []chapterMeta {
	chapters := make([]chapterMeta, 0, len(infos))
	for _, info := range infos {
		chapters = append(chapters, chapterMeta{UID: info.ChapterUID, Idx: info.ChapterIdx, Title: info.Title})
	}
	return chapters
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
	pages, err := a.paginateChapter(text)
	if err != nil {
		a.fail(fmt.Sprintf("分页失败: %v", err))
		return
	}
	if len(pages) == 0 {
		pages = []Page{{Start: 0, End: len(text)}}
	}
	a.pages = pages
	a.readerPage = clamp(page, 0, len(a.pages)-1)
	a.catSel = idx // 阅读跨章后目录选中项跟着走，进度和返回才一致
	a.refreshBookSizes()
	a.view = viewReader
	a.saveCurrentProgress()
}

// handleReader 阅读器按键。
func (a *app) handleReader(ev c1device.Event) bool {
	switch ev.Key {
	case c1device.KeyUp:
		if a.readerPage > 0 {
			a.readerPage--
			a.saveCurrentProgress()
		} else if a.catSel > 0 {
			a.openChapter(a.catSel-1, lastPage(a, a.catSel-1))
			a.saveCurrentProgress()
		}
	case c1device.KeyDown:
		if a.readerPage < len(a.pages)-1 {
			a.readerPage++
			a.saveCurrentProgress()
		} else if a.catSel < len(a.chapters)-1 {
			a.openChapter(a.catSel+1, 0)
			a.saveCurrentProgress()
		}
	case c1device.KeyOK:
		a.toggleFullscreen()
	case c1device.KeyLeft, c1device.KeyBack:
		a.saveCurrentProgress()
		a.view = viewCatalog
	case c1device.KeyRune:
		switch ev.Rune {
		case 's', 'S':
			a.view = viewSettings
			a.settingsSel = 0
		case 'f', 'F':
			a.toggleFullscreen()
		}
	}
	return false
}

// saveCurrentProgress 保存当前书进度并刷新 Updated 时间戳。
func (a *app) saveCurrentProgress() {
	if a.currentBook == nil || a.catSel < 0 || a.catSel >= len(a.chapters) {
		return
	}
	chapterIdx := a.chapters[a.catSel].Idx
	a.progress[a.currentBook.BookID] = bookProgress{
		ChapterIdx: chapterIdx,
		Page:       a.readerPage,
		Updated:    time.Now().Unix(),
	}
	if err := saveProgress(a.progress); err != nil {
		fmt.Fprintln(os.Stderr, "c1weread: save progress:", err)
	}
}

// toggleFullscreen 切换阅读器全屏模式，重分页并保持当前页首字节偏移。
func (a *app) toggleFullscreen() {
	if a.view != viewReader || len(a.pages) == 0 {
		return
	}
	a.fullscreen = !a.fullscreen
	a.repageCurrentChapter()
}

// lastPage 计算某章最后一页（用于上翻跨章）。
func lastPage(a *app, idx int) int {
	ch := a.chapters[idx]
	text, err := chapterText(a.currentBook, ch)
	if err != nil {
		return 0
	}
	pages, err := paginateWidth(text, a.bodyFace, a.readerBodyWidthFor(), a.readerBodyHeight())
	if err != nil || len(pages) == 0 {
		return 0
	}
	return len(pages) - 1
}

// readerBodyTop 返回正文区顶部 y 坐标（全屏留 4px 边距，同 reader-lab fullscreenBodyTop）。
func (a *app) readerBodyTop() int {
	if a.fullscreen {
		return 4
	}
	return rowH
}

// readerBodyHeight 返回当前正文区高度（随方向变化；全屏时为整屏减边距）。
func (a *app) readerBodyHeight() int {
	if a.fullscreen {
		return a.uiH() - 2*4
	}
	return a.uiH() - 2*rowH
}

// paginateChapter 使用当前正文区高度对当前章节分页。
func (a *app) paginateChapter(text string) ([]Page, error) {
	return paginateWidth(text, a.bodyFace, a.readerBodyWidthFor(), a.readerBodyHeight())
}

// repageCurrentChapter 重新分页并尽量保持当前页首字节偏移。
func (a *app) repageCurrentChapter() {
	if a.currentBook == nil || a.catSel < 0 || a.catSel >= len(a.chapters) {
		return
	}
	offset := 0
	if a.readerPage >= 0 && a.readerPage < len(a.pages) {
		offset = a.pages[a.readerPage].Start
	}
	text, err := chapterText(a.currentBook, a.chapters[a.catSel])
	if err != nil {
		a.message = sanitizeMessage(err.Error())
		return
	}
	pages, err := a.paginateChapter(text)
	if err != nil {
		a.message = sanitizeMessage(err.Error())
		return
	}
	if len(pages) == 0 {
		pages = []Page{{Start: 0, End: len(text)}}
	}
	a.pages = pages
	a.readerPage = pageIndexAt(a.pages, offset)
}

// chapterIndexForIdx 把章节 Idx 映射到 chapters 切片下标。
func (a *app) chapterIndexForIdx(idx int) int {
	for i, ch := range a.chapters {
		if ch.Idx == idx {
			return i
		}
	}
	return -1
}

// refreshBookSizes 统计各章节落盘字节（前缀和）与全书总字节，供顶栏百分比使用。
func (a *app) refreshBookSizes() {
	a.bookPrefix, a.bookTotal = nil, 0
	if a.currentBook == nil {
		return
	}
	a.bookTotal = a.currentBook.TotalBytes
	n := len(a.chapters)
	a.bookPrefix = make([]int64, n)
	sum := int64(0)
	for i, ch := range a.chapters {
		a.bookPrefix[i] = sum
		if fi, err := os.Stat(chapterFile(a.currentBook.BookID, ch)); err == nil {
			sum += fi.Size()
		}
	}
	if a.bookTotal < sum {
		// 记录的总字节比已落盘的还小（删过章节文件等异常），回退到章节比例估算。
		a.bookTotal = 0
	}
}

// bookPercent 估算全书阅读百分比。整本下载过（manifest.totalBytes > 0）按字节
// （之前章节落盘字节合计 + 当前页在章内的字节偏移）；否则按章节序号比例回退。
func (a *app) bookPercent() float64 {
	n := len(a.chapters)
	if n == 0 || a.catSel < 0 || a.catSel >= n {
		return 0
	}
	pageFrac := 0.0
	if len(a.pages) > 0 {
		pageFrac = float64(clamp(a.readerPage, 0, len(a.pages)-1)) / float64(len(a.pages))
	}
	if a.bookTotal > 0 && len(a.bookPrefix) == n {
		pos := a.bookPrefix[a.catSel]
		if a.readerPage >= 0 && a.readerPage < len(a.pages) {
			pos += int64(a.pages[a.readerPage].Start)
		}
		if pos > a.bookTotal {
			pos = a.bookTotal
		}
		return float64(pos) * 100 / float64(a.bookTotal)
	}
	return (float64(a.catSel) + pageFrac) * 100 / float64(n)
}

// openRecent 进入最近阅读视图。
func (a *app) openRecent() {
	a.recentBooks = a.buildRecentBooks()
	a.recentSel, a.recentFirst = 0, 0
	a.view = viewRecent
}

// buildRecentBooks 按 Updated 倒序构造最近阅读列表，最多 10 条。
func (a *app) buildRecentBooks() []recentBook {
	books := make([]recentBook, 0, len(a.progress))
	for bookID, p := range a.progress {
		m, _ := loadManifest(bookID)
		title := bookID
		chapterTitle := ""
		if m != nil && m.Title != "" {
			title = m.Title
		}
		if m != nil {
			for _, ch := range m.Chapters {
				if ch.Idx == p.ChapterIdx {
					chapterTitle = ch.Title
					break
				}
			}
		}
		books = append(books, recentBook{
			BookID:       bookID,
			Title:        title,
			ChapterTitle: chapterTitle,
			ChapterIdx:   p.ChapterIdx,
			Page:         p.Page,
			Updated:      p.Updated,
		})
	}
	sort.Slice(books, func(i, j int) bool {
		if books[i].Updated == books[j].Updated {
			return books[i].BookID < books[j].BookID
		}
		return books[i].Updated > books[j].Updated
	})
	if len(books) > 10 {
		books = books[:10]
	}
	return books
}

// handleRecent 最近阅读视图按键。
func (a *app) handleRecent(ev c1device.Event) bool {
	n := len(a.recentBooks)
	switch ev.Key {
	case c1device.KeyUp:
		if n > 0 && a.recentSel > 0 {
			a.recentSel--
			if a.recentSel < a.recentFirst {
				a.recentFirst = a.recentSel
			}
		}
	case c1device.KeyDown:
		if n > 0 && a.recentSel < n-1 {
			a.recentSel++
			if a.recentSel >= a.recentFirst+listRows {
				a.recentFirst = a.recentSel - listRows + 1
			}
		}
	case c1device.KeyOK:
		if n == 0 {
			return false
		}
		r := a.recentBooks[a.recentSel]
		// 书架中定位到该书，方便返回时选中一致
		for i, b := range a.shelf {
			if b.BookID == r.BookID {
				a.shelfSel = i
				if a.shelfSel >= a.shelfFirst+listRows {
					a.shelfFirst = a.shelfSel - listRows + 1
				}
				break
			}
		}
		a.openRecentBook(r)
	case c1device.KeyLeft, c1device.KeyBack:
		a.view = viewShelf
	}
	return false
}

// openRecentBook 从最近阅读打开一本书，复用带进度续读逻辑。
func (a *app) openRecentBook(r recentBook) {
	// 确保 progress 中存在该记录
	a.progress[r.BookID] = bookProgress{ChapterIdx: r.ChapterIdx, Page: r.Page, Updated: r.Updated}
	var book *weread.ShelfBook
	for i := range a.shelf {
		if a.shelf[i].BookID == r.BookID {
			book = &a.shelf[i]
			break
		}
	}
	if book == nil {
		// 不在当前书架时构造最小信息
		book = &weread.ShelfBook{BookID: r.BookID, Title: r.Title}
	}
	a.openBook(*book)
}

// handleSettings 设置视图按键。
func (a *app) handleSettings(ev c1device.Event) bool {
	switch ev.Key {
	case c1device.KeyBack, c1device.KeyRune:
		if ev.Key == c1device.KeyBack || ev.Rune == 's' || ev.Rune == 'S' {
			a.view = viewReader
		}
	case c1device.KeyUp:
		if a.settingsSel > 0 {
			a.settingsSel--
		}
	case c1device.KeyDown:
		if a.settingsSel < 3 {
			a.settingsSel++
		}
	case c1device.KeyLeft, c1device.KeyRight:
		delta := 1
		if ev.Key == c1device.KeyLeft {
			delta = -1
		}
		settings := a.settings
		switch a.settingsSel {
		case 0:
			fonts := []string{fontUnifont, fontFusion12, fontLXGW, fontMiSans, fontNeoXiHei}
			pick := 0
			for i, f := range fonts {
				if f == settings.Font {
					pick = i
					break
				}
			}
			pick += delta
			if pick < 0 {
				pick = len(fonts) - 1
			}
			if pick >= len(fonts) {
				pick = 0
			}
			settings.Font = fonts[pick]
			if settings.Font == fontFusion12 {
				settings.Size = 12
			}
		case 1:
			if settings.Font == fontFusion12 {
				a.message = "Fusion 原生点阵固定 12px"
				return false
			}
			for i, size := range readerFontSizes {
				if size == settings.Size {
					next := i + delta
					if next < 0 {
						next = len(readerFontSizes) - 1
					}
					if next >= len(readerFontSizes) {
						next = 0
					}
					settings.Size = readerFontSizes[next]
					break
				}
			}
		case 2:
			settings.Threshold += delta * 16
			if settings.Threshold < 32 {
				settings.Threshold = 32
			}
			if settings.Threshold > 224 {
				settings.Threshold = 224
			}
		case 3:
			next := int(settings.Rotate) + delta
			if next < 0 {
				next = 3
			}
			if next > 3 {
				next = 0
			}
			settings.Rotate = c1device.Orientation(next)
		}
		if settings != a.settings {
			if err := a.applyReaderSettings(settings, true); err != nil {
				a.message = sanitizeMessage(err.Error())
			}
		}
	}
	return false
}

// applyReaderSettings 应用新设置：按需创建 Face，重分页并尽量保持当前页首位置，持久化。
func (a *app) applyReaderSettings(settings ReaderSettings, persist bool) error {
	if err := settings.validate(); err != nil {
		return err
	}
	needFace := settings.Font != a.settings.Font || settings.Size != a.settings.Size || a.bodyFace == nil
	var (
		face *c1device.Face
		data *mappedFont
		err  error
	)
	if needFace {
		face, data, err = newReaderFace(settings)
		if err != nil {
			// 运行时字体缺失时回退到内嵌 Unifont
			if settings.Font != fontUnifont {
				a.message = sanitizeMessage(err.Error()) + "，已回退 Unifont"
				settings = ReaderSettings{Font: fontUnifont, Size: 16, Threshold: settings.Threshold, Rotate: settings.Rotate}
				face, data, err = newReaderFace(settings)
			}
			if err != nil {
				return err
			}
		}
	} else {
		face, data = a.bodyFace, a.fontData
	}

	oldFace, oldData, oldSettings := a.bodyFace, a.fontData, a.settings
	a.bodyFace, a.fontData, a.settings = face, data, settings

	if a.currentBook != nil && a.catSel >= 0 && a.catSel < len(a.chapters) && (a.view == viewReader || a.view == viewSettings || a.view == viewCatalog) {
		var offset int
		if a.readerPage >= 0 && a.readerPage < len(a.pages) {
			offset = a.pages[a.readerPage].Start
		}
		ch := a.chapters[a.catSel]
		text, err := chapterText(a.currentBook, ch)
		if err != nil {
			// 保持旧状态
			a.bodyFace, a.fontData, a.settings = oldFace, oldData, oldSettings
			if needFace {
				face.Close()
				if data != nil {
					data.Close()
				}
			}
			return fmt.Errorf("重读章节失败: %w", err)
		}
		pages, err := a.paginateChapter(text)
		if err != nil {
			a.bodyFace, a.fontData, a.settings = oldFace, oldData, oldSettings
			if needFace {
				face.Close()
				if data != nil {
					data.Close()
				}
			}
			return fmt.Errorf("重分页失败: %w", err)
		}
		if len(pages) == 0 {
			pages = []Page{{Start: 0, End: len(text)}}
		}
		a.pages = pages
		a.readerPage = pageIndexAt(a.pages, offset)
	}

	if persist {
		if err := saveReaderSettings(a.settings); err != nil {
			// 回滚已应用的字体/分页，保持文件与内存一致
			a.bodyFace, a.fontData, a.settings = oldFace, oldData, oldSettings
			if needFace {
				face.Close()
				if data != nil {
					data.Close()
				}
			}
			return fmt.Errorf("保存设置失败: %w", err)
		}
	}

	if needFace {
		if oldFace != nil {
			oldFace.Close()
		}
		if oldData != nil {
			oldData.Close()
		}
	}
	return nil
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
