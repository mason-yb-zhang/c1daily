package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestReaderSettingsValidation(t *testing.T) {
	cases := []struct {
		name string
		s    ReaderSettings
		ok   bool
	}{
		{"default", defaultReaderSettings(), true},
		{"unifont-12", ReaderSettings{Font: fontUnifont, Size: 12, Threshold: 128}, true},
		{"unifont-24", ReaderSettings{Font: fontUnifont, Size: 24, Threshold: 128}, true},
		{"fusion-locked-12", ReaderSettings{Font: fontFusion12, Size: 12, Threshold: 128}, true},
		{"fusion-other-size", ReaderSettings{Font: fontFusion12, Size: 14, Threshold: 128}, false},
		{"lxgw", ReaderSettings{Font: fontLXGW, Size: 18, Threshold: 128}, true},
		{"bad-threshold-low", ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 31}, false},
		{"bad-threshold-high", ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 225}, false},
		{"threshold-boundaries", ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 32}, true},
		{"threshold-boundaries-224", ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 224}, true},
		{"bad-font", ReaderSettings{Font: "comic", Size: 16, Threshold: 128}, false},
		{"bad-size", ReaderSettings{Font: fontUnifont, Size: 13, Threshold: 128}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.s.validate()
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestReaderSettingsPersistence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("C1DAILY_DATA", dir)

	want := ReaderSettings{Font: fontLXGW, Size: 20, Threshold: 160}
	if err := saveReaderSettings(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadReaderSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("loaded %+v, want %+v", got, want)
	}

	// corrupted JSON should fall back to defaults with error
	if err := os.WriteFile(readerSettingsPath(), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = loadReaderSettings()
	if err == nil {
		t.Fatal("expected error for invalid settings")
	}
	if got != defaultReaderSettings() {
		t.Fatalf("expected default fallback, got %+v", got)
	}
}

func TestNewReaderFaceUnifontSizes(t *testing.T) {
	for _, size := range readerFontSizes {
		settings := ReaderSettings{Font: fontUnifont, Size: size, Threshold: 128}
		face, data, err := newReaderFace(settings)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		face.Close()
		if data != nil {
			data.Close()
		}
	}
}

func TestMiSansFallbackWhenMissing(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("C1DAILY_FONTS", emptyDir)

	settings := ReaderSettings{Font: fontMiSans, Size: 14, Threshold: 128}
	_, _, err := newReaderFace(settings)
	if err == nil {
		t.Fatal("expected error when ttf is missing")
	}

	a := &app{}
	if err := a.applyReaderSettings(settings, false); err != nil {
		t.Fatalf("apply should not fail: %v", err)
	}
	if a.settings.Font != fontUnifont {
		t.Fatalf("expected fallback to unifont, got %+v", a.settings)
	}
	if a.bodyFace == nil {
		t.Fatal("expected bodyFace after fallback")
	}
	a.bodyFace.Close()
	if a.fontData != nil {
		a.fontData.Close()
	}
}

func TestPaginateWithUnifont(t *testing.T) {
	face, data, err := newReaderFace(ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 128})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	if data != nil {
		defer data.Close()
	}

	text := "chapter one\n" + repeatString("abcdefghijklmnopqrstuvwxyz\n", 50)
	pages, err := paginate(text, face, readerBodyHeight)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 {
		t.Fatal("expected pages")
	}
	first := pages[0]
	if first.Start != 0 {
		t.Fatalf("first page start = %d, want 0", first.Start)
	}
	for i := 1; i < len(pages); i++ {
		if pages[i].Start != pages[i-1].End {
			t.Fatalf("page boundary mismatch: page %d end=%d, page %d start=%d", i-1, pages[i-1].End, i, pages[i].Start)
		}
	}
}

func TestPaginateWithFusion12(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "repos", "reader-lab", "App", "book-reader", "assets", "fusion12.bin")
	if _, err := os.Stat(src); err != nil {
		t.Skip("fusion12.bin not available:", err)
	}
	dir := t.TempDir()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fusion12FileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("C1DAILY_FONTS", dir)

	face, mapped, err := newReaderFace(ReaderSettings{Font: fontFusion12, Size: 12, Threshold: 128})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	if mapped != nil {
		defer mapped.Close()
	}

	text := repeatString("测试 Fusion 12px 点阵分页的中文文本。", 80)
	pages, err := paginate(text, face, readerBodyHeight)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 2 {
		t.Fatalf("expected multiple pages, got %d", len(pages))
	}
}

func TestPaginateMixedASCII(t *testing.T) {
	face, data, err := newReaderFace(ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 128})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	if data != nil {
		defer data.Close()
	}

	text := "Hello world 123abc test."
	pages, err := paginate(text, face, readerBodyHeight)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 {
		t.Fatal("expected at least one page")
	}
	joined := ""
	for _, p := range pages {
		for _, l := range p.Lines {
			joined += l
		}
	}
	if joined != text {
		t.Fatalf("roundtrip mismatch: got %q want %q", joined, text)
	}
}

func repeatString(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

// TestCacheRoundTrip 文章落盘 + 索引更新 + 读回。
func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("C1DAILY_DATA", dir)

	article := &Article{
		Title:      "Test Title",
		Author:     "Tester",
		Paragraphs: []string{"first paragraph body text", "second paragraph body text"},
		FetchedAt:  1729430400,
		Source:     dushuSourceToday,
	}
	if err := cacheArticle("2026-10-21", article); err != nil {
		t.Fatal(err)
	}
	got, err := loadArticle("2026-10-21")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != article.Title || len(got.Paragraphs) != 2 {
		t.Fatalf("loaded %+v", got)
	}
	entries, err := loadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Key != "2026-10-21" {
		t.Fatalf("index = %+v", entries)
	}
}

// TestCacheLRUEviction 超过 30 条时最旧条目被淘汰（含文章文件）。
func TestCacheLRUEviction(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("C1DAILY_DATA", dir)

	for i := 0; i < maxCachedArticles+5; i++ {
		a := &Article{
			Title:      fmt.Sprintf("Article %03d", i),
			Paragraphs: []string{"paragraph body text here"},
			FetchedAt:  int64(1729430400 + i),
			Source:     dushuSourceRandom,
		}
		if err := cacheArticle(fmt.Sprintf("key-%03d", i), a); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := loadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxCachedArticles {
		t.Fatalf("index len = %d, want %d", len(entries), maxCachedArticles)
	}
	// 最旧的 5 条应被淘汰
	if _, err := os.Stat(articlePath("key-000")); !os.IsNotExist(err) {
		t.Fatalf("oldest article file should be evicted, stat err = %v", err)
	}
	// 最新的应保留且排在最前
	if entries[0].Key != fmt.Sprintf("key-%03d", maxCachedArticles+4) {
		t.Fatalf("newest entry = %q", entries[0].Key)
	}
	// 重抓已有 key 只更新不追加
	a := &Article{Title: "Refresh", Paragraphs: []string{"text"}, FetchedAt: 1729439999, Source: "x"}
	if err := cacheArticle(entries[0].Key, a); err != nil {
		t.Fatal(err)
	}
	entries2, err := loadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries2) != maxCachedArticles {
		t.Fatalf("index len after refresh = %d", len(entries2))
	}
}
