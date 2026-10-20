package main

import (
	"os"
	"testing"
	"time"
)

// TestParseTodayFixture 用真实抓取的今日页面验证解析。
func TestParseTodayFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/today.html")
	if err != nil {
		t.Skip("fixture missing:", err)
	}
	a, err := parseDushuPage(body, dushuSourceToday)
	if err != nil {
		t.Fatal(err)
	}
	if a.Title == "" {
		t.Fatal("empty title")
	}
	if a.Author == "" {
		t.Fatal("empty author")
	}
	if len(a.Paragraphs) == 0 {
		t.Fatal("no paragraphs")
	}
	for i, p := range a.Paragraphs {
		if len(p) < 10 {
			t.Fatalf("paragraph %d too short: %q", i, p)
		}
	}
	if a.Source != dushuSourceToday {
		t.Fatalf("source = %q", a.Source)
	}
	if a.FetchedAt == 0 {
		t.Fatal("fetchedAt not set")
	}
	t.Logf("title=%q author=%q paragraphs=%d", a.Title, a.Author, len(a.Paragraphs))
}

// TestParseRandomFixture 用真实抓取的随机页面验证解析。
func TestParseRandomFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/random.html")
	if err != nil {
		t.Skip("fixture missing:", err)
	}
	a, err := parseDushuPage(body, dushuSourceRandom)
	if err != nil {
		t.Fatal(err)
	}
	if a.Title == "" {
		t.Fatal("empty title")
	}
	if len(a.Paragraphs) == 0 {
		t.Fatal("no paragraphs")
	}
	if a.Source != dushuSourceRandom {
		t.Fatalf("source = %q", a.Source)
	}
	t.Logf("title=%q author=%q paragraphs=%d", a.Title, a.Author, len(a.Paragraphs))
}

// TestParseGarbage 非法 HTML 应报错而不是 panic。
func TestParseGarbage(t *testing.T) {
	if _, err := parseDushuPage([]byte("<html><body>no article here</body></html>"), "x"); err == nil {
		t.Fatal("expected error for page without article")
	}
}

// TestRandomKeyStable 同一文章的 key 稳定，不同文章 key 不同。
func TestRandomKeyStable(t *testing.T) {
	a := &Article{Title: "Some Title", Paragraphs: []string{"first paragraph text", "second"}}
	b := &Article{Title: "Some Title", Paragraphs: []string{"first paragraph text", "second"}}
	c := &Article{Title: "Other Title", Paragraphs: []string{"first paragraph text"}}
	if randomKey(a) != randomKey(b) {
		t.Fatal("key not stable")
	}
	if randomKey(a) == randomKey(c) {
		t.Fatal("key collision across articles")
	}
	if len(randomKey(a)) != 16 {
		t.Fatalf("key length = %d, want 16", len(randomKey(a)))
	}
}

// TestTodayKeyFormat YYYY-MM-DD。
func TestTodayKeyFormat(t *testing.T) {
	key := todayKey(time.Now())
	if len(key) != 10 || key[4] != '-' || key[7] != '-' {
		t.Fatalf("bad today key: %q", key)
	}
}

// TestFetchLive 联网真实抓取（默认跳过；C1DAILY_LIVE_TEST=1 时启用）。
func TestFetchLive(t *testing.T) {
	if !liveEnabled() {
		t.Skip("set C1DAILY_LIVE_TEST=1 to run live fetch test")
	}
	a, err := FetchToday()
	if err != nil {
		t.Fatal(err)
	}
	if a.Title == "" || len(a.Paragraphs) == 0 {
		t.Fatal("incomplete article")
	}
	t.Logf("today: title=%q author=%q paragraphs=%d", a.Title, a.Author, len(a.Paragraphs))
	r, err := FetchRandom()
	if err != nil {
		t.Fatal(err)
	}
	if r.Title == "" || len(r.Paragraphs) == 0 {
		t.Fatal("incomplete random article")
	}
	t.Logf("random: title=%q author=%q paragraphs=%d", r.Title, r.Author, len(r.Paragraphs))
}

func liveEnabled() bool {
	return os.Getenv("C1DAILY_LIVE_TEST") == "1"
}
