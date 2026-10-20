// 读书网（dushu.com）每日美文抓取与解析。
// 页面为 UTF-8 完整 HTML；站点偶发慢/超时，HTTP client 超时 30 秒、失败重试 1 次。
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	dushuTodayURL  = "https://www.dushu.com/meiwen/"
	dushuRandomURL = "https://www.dushu.com/meiwen/random/"

	dushuSourceToday  = "dushu-today"
	dushuSourceRandom = "dushu-random"

	dushuHTTPTimeout = 30 * time.Second
	dushuRetries     = 1 // 失败后额外重试次数
	dushuMaxBody     = 1 << 20
)

// dushuClient 站点较慢，超时放宽到 30 秒。
var dushuClient = &http.Client{Timeout: dushuHTTPTimeout}

// FetchToday 抓取今日美文（/meiwen/）。
func FetchToday() (*Article, error) {
	return fetchDushu(dushuTodayURL, dushuSourceToday)
}

// FetchRandom 抓取随机美文（/meiwen/random/，每次返回不同文章）。
func FetchRandom() (*Article, error) {
	return fetchDushu(dushuRandomURL, dushuSourceRandom)
}

// fetchDushu 抓取并解析文章；失败重试 dushuRetries 次。
func fetchDushu(url, source string) (*Article, error) {
	var lastErr error
	for attempt := 0; attempt <= dushuRetries; attempt++ {
		body, err := dushuGet(url)
		if err != nil {
			lastErr = err
			continue
		}
		article, err := parseDushuPage(body, source)
		if err != nil {
			lastErr = err
			continue
		}
		return article, nil
	}
	return nil, fmt.Errorf("fetch %s: %w", source, lastErr)
}

func dushuGet(url string) ([]byte, error) {
	resp, err := dushuClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, dushuMaxBody))
}

// todayKey 今日文章的缓存 key：本地日期 YYYY-MM-DD（设备本地时区）。
func todayKey(now time.Time) string {
	return now.Format("2006-01-02")
}

// randomKey 随机文章的缓存 key：sha1(标题+首段) 前 16 个十六进制字符。
func randomKey(a *Article) string {
	h := sha1.New()
	h.Write([]byte(a.Title))
	if len(a.Paragraphs) > 0 {
		h.Write([]byte(a.Paragraphs[0]))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// parseDushuPage 从完整 HTML 页面解析标题 / 作者 / 正文段落。
// 结构（已用真实页面验证）：
//   - 标题：`<h1 style="text-align:center;">标题</h1>`
//   - 作者：`<div class="article-infos">` 内 `<div class="article-info">` 的 `<span>作者</span>`
//   - 正文：`<div class="text" ...>` 内的所有 `<p>` 段落
func parseDushuPage(body []byte, source string) (*Article, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	article := &Article{Source: source, FetchedAt: time.Now().Unix()}
	var textDiv *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h1":
				if strings.Contains(attr(n, "style"), "text-align:center") && article.Title == "" {
					article.Title = strings.TrimSpace(nodeText(n))
				}
			case "div":
				cls := attr(n, "class")
				if article.Author == "" && strings.Contains(cls, "article-infos") {
					article.Author = extractAuthor(n)
				}
				if textDiv == nil && hasClass(n, "text") {
					textDiv = n
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if textDiv != nil {
		article.Paragraphs = extractParagraphs(textDiv)
	}
	if article.Title == "" {
		return nil, fmt.Errorf("title not found")
	}
	if len(article.Paragraphs) == 0 {
		return nil, fmt.Errorf("paragraphs not found")
	}
	return article, nil
}

// extractAuthor 在 article-infos 子树里找第一个 span 的文本。
func extractAuthor(n *html.Node) string {
	var findSpan func(*html.Node) string
	findSpan = func(m *html.Node) string {
		if m.Type == html.ElementNode && m.Data == "span" {
			return strings.TrimSpace(nodeText(m))
		}
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			if s := findSpan(c); s != "" {
				return s
			}
		}
		return ""
	}
	return findSpan(n)
}

// extractParagraphs 收集 text div 内所有 <p> 的纯文本段落。
func extractParagraphs(n *html.Node) []string {
	var out []string
	var walk func(*html.Node)
	walk = func(m *html.Node) {
		if m.Type == html.ElementNode && m.Data == "p" {
			if text := strings.TrimSpace(nodeText(m)); text != "" {
				out = append(out, text)
			}
			return
		}
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

// nodeText 返回子树的纯文本。
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(m *html.Node) {
		if m.Type == html.TextNode {
			b.WriteString(m.Data)
		}
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// hasClass 判断元素的 class 属性是否包含指定 token。
func hasClass(n *html.Node, want string) bool {
	for _, field := range strings.Fields(attr(n, "class")) {
		if field == want {
			return true
		}
	}
	return false
}
