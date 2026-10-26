package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"unicode/utf8"

	"c1device"
	"c1weread/weread"
)

func TestChapterTextRejectsEmptyCache(t *testing.T) {
	t.Setenv("C1WEREAD_DATA", t.TempDir())
	m := &manifest{BookID: "book-empty", Title: "测试", Chapters: []chapterMeta{{UID: "1", Idx: 1, Title: "第一章"}}}
	if err := saveManifest(m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chapterFile(m.BookID, m.Chapters[0]), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := chapterText(m, m.Chapters[0]); err == nil {
		t.Fatal("chapterText() accepted an empty cache file")
	}
}

func TestLoadAndMergeLocalShelf(t *testing.T) {
	t.Setenv("C1WEREAD_DATA", t.TempDir())
	for _, m := range []*manifest{
		{BookID: "local-b", Title: "本地乙", Chapters: []chapterMeta{{UID: "1", Idx: 1}}},
		{BookID: "local-a", Title: "本地甲", Author: "作者", Chapters: []chapterMeta{{UID: "1", Idx: 1}}},
		{BookID: "broken", Title: "无目录"},
	} {
		if err := saveManifest(m); err != nil {
			t.Fatal(err)
		}
	}
	local := loadLocalShelf()
	if len(local) != 2 || local[0].BookID != "local-a" || local[1].BookID != "local-b" {
		t.Fatalf("loadLocalShelf() = %+v", local)
	}
	online := []weread.ShelfBook{{BookID: "online", Title: "在线书"}, {BookID: "local-a", Title: "在线书名"}}
	merged := mergeShelfBooks(online, local)
	if len(merged) != 3 || merged[0].BookID != "online" || merged[1].Title != "在线书名" || merged[2].BookID != "local-b" {
		t.Fatalf("mergeShelfBooks() = %+v", merged)
	}
}

func TestDownloadBookRefreshesExistingCatalogWithoutMutatingApp(t *testing.T) {
	t.Setenv("C1WEREAD_DATA", t.TempDir())
	m := &manifest{BookID: "book-refresh", Title: "连载", Chapters: []chapterMeta{{UID: "1", Idx: 1, Title: "旧标题"}}}
	if err := saveManifest(m); err != nil {
		t.Fatal(err)
	}
	if err := writeChapterText(m, m.Chapters[0], "已有正文"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/web/book/chapterInfos", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{
			"bookId":  m.BookID,
			"updated": []any{map[string]any{"chapterUid": 1, "chapterIdx": 1, "title": "新标题", "wordCount": 100}},
		}}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := weread.NewClient(weread.NewJar(), nil)
	client.HTTP = srv.Client()
	client.BaseURL = srv.URL

	a := &app{client: client, downloadTotal: 99, downloadDone: 99}
	events := make(chan downloadEvent, 8)
	a.downloadBook(context.Background(), m, events)
	close(events)
	var final downloadEvent
	for ev := range events {
		if ev.finished {
			final = ev
		}
	}
	if final.err != nil || !final.finished {
		t.Fatalf("download final event = %+v", final)
	}
	if len(m.Chapters) != 1 || m.Chapters[0].Title != "新标题" {
		t.Fatalf("catalog was not refreshed: %+v", m.Chapters)
	}
	if a.downloadTotal != 99 || a.downloadDone != 99 || a.currentBook != nil || a.chapters != nil {
		t.Fatalf("background download mutated app state: total=%d done=%d current=%v chapters=%v", a.downloadTotal, a.downloadDone, a.currentBook, a.chapters)
	}
}

func TestTruncateToWidthPreservesUTF8(t *testing.T) {
	face, err := c1device.NewBitmapFace(fontData, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	for width := 1; width < 200; width++ {
		got := truncateToWidth(face, "这是一段很长很长很长的中文标题", width)
		if !utf8.ValidString(got) {
			t.Fatalf("width=%d produced invalid UTF-8: %q", width, got)
		}
	}
}
