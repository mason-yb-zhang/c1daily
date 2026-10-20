// 读书网（dushu.com）每日美文抓取与解析。
// 页面为 UTF-8 完整 HTML；站点偶发慢（观测到 40s 无响应），HTTP client 超时 60 秒、
// 失败重试 2 次（间隔 2s）。
// 设备的 /etc/ssl/certs 为空（系统无 CA 根证书），故内嵌 Certum 根/中间证书自建 pool。
package main

import (
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
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

	dushuHTTPTimeout  = 60 * time.Second
	dushuRetries      = 2 // 失败后额外重试次数
	dushuRetryBackoff = 2 * time.Second
	dushuMaxBody      = 1 << 20
)

//go:embed assets/tls/certum-trusted-root-ca.pem
var certumRootPEM []byte

//go:embed assets/tls/certum-dv-tls-g2-r39-ca.pem
var certumIntermediatePEM []byte

// dushuCertPool 内嵌 Certum 根证书 + 中间证书（服务端有发送，双保险）构建的
// x509 pool。设备 /etc/ssl/certs 为空，x509.SystemCertPool 取不到根证书，
// 因此必须先尝试系统 pool（失败或为空则新建），再追加两个 PEM。
func dushuCertPool() (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if len(pool.Subjects()) == 0 {
		// 设备上 SystemCertPool 可能返回空 pool 而不报错，同样不能用。
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(certumRootPEM) {
		return nil, fmt.Errorf("append certum root ca failed")
	}
	if !pool.AppendCertsFromPEM(certumIntermediatePEM) {
		return nil, fmt.Errorf("append certum intermediate ca failed")
	}
	return pool, nil
}

// dushuTLSConfig 设备实测：Go 默认 TLS 1.3（含后量子 X25519Kyber768 密钥协商，
// ClientHello 很大）会让握手在设备网络路径上停滞直到超时（curl TLS 1.2 正常）。
// 强制 TLS 1.2 规避；RootCAs 用内嵌 Certum pool（设备系统无 CA 根证书）。
var dushuTLSConfig = &tls.Config{
	RootCAs:    mustDushuCertPool(),
	MaxVersion: tls.VersionTLS12,
}

// dushuClient 站点较慢，超时放宽到 60 秒。
var dushuClient = &http.Client{
	Timeout:   dushuHTTPTimeout,
	Transport: &http.Transport{TLSClientConfig: dushuTLSConfig},
}

func mustDushuCertPool() *x509.CertPool {
	pool, err := dushuCertPool()
	if err != nil {
		// 内嵌 PEM 是编译期资产，构建失败只能是编码错误；panic 留痕比带病运行好。
		panic(fmt.Sprintf("c1daily: build cert pool: %v", err))
	}
	return pool
}

// FetchToday 抓取今日美文（/meiwen/）。
func FetchToday() (*Article, error) {
	return fetchDushu(dushuTodayURL, dushuSourceToday)
}

// FetchRandom 抓取随机美文（/meiwen/random/，每次返回不同文章）。
func FetchRandom() (*Article, error) {
	return fetchDushu(dushuRandomURL, dushuSourceRandom)
}

// fetchDushu 抓取并解析文章；失败重试 dushuRetries 次（间隔 dushuRetryBackoff）。
func fetchDushu(url, source string) (*Article, error) {
	var lastErr error
	for attempt := 0; attempt <= dushuRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(dushuRetryBackoff)
		}
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
