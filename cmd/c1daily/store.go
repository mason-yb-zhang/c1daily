// 本地存储：数据目录、文章缓存（articles/<key>.json）、缓存索引 index.json。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const defaultDataDir = "/storage/c1/local-apps/apps/c1daily/data"

// maxCachedArticles 缓存列表上限，超出按 LRU（fetchedAt 最早）淘汰。
const maxCachedArticles = 30

// dataDir 返回数据目录（C1DAILY_DATA 可覆盖，便于 PC 预览测试）。
func dataDir() string {
	if dir := os.Getenv("C1DAILY_DATA"); dir != "" {
		return dir
	}
	return defaultDataDir
}

func ensureDataDir() error {
	return os.MkdirAll(dataDir(), 0o755)
}

func articlesDir() string { return filepath.Join(dataDir(), "articles") }
func indexPath() string   { return filepath.Join(dataDir(), "index.json") }

// Article 一篇文章：标题 / 作者 / 正文段落 / 抓取时间 / 来源。
type Article struct {
	Title      string   `json:"title"`
	Author     string   `json:"author"`
	Paragraphs []string `json:"paragraphs"`
	FetchedAt  int64    `json:"fetchedAt"`
	Source     string   `json:"source"`
}

// indexEntry 缓存索引条目（index.json 的数组元素），按 FetchedAt 倒序。
type indexEntry struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	FetchedAt int64  `json:"fetchedAt"`
	Source    string `json:"source"`
}

func articlePath(key string) string {
	return filepath.Join(articlesDir(), key+".json")
}

// loadArticle 读缓存文章；缺失返回 (nil, nil)。
func loadArticle(key string) (*Article, error) {
	data, err := os.ReadFile(articlePath(key))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a Article
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("parse article %s: %w", key, err)
	}
	if len(a.Paragraphs) == 0 {
		return nil, fmt.Errorf("article %s: no paragraphs", key)
	}
	return &a, nil
}

// cacheArticle 把文章落盘 articles/<key>.json，并更新索引（去重 + LRU 淘汰）。
func cacheArticle(key string, a *Article) error {
	if err := os.MkdirAll(articlesDir(), 0o755); err != nil {
		return err
	}
	if err := atomicWrite(articlePath(key), a); err != nil {
		return err
	}
	return addIndexEntry(indexEntry{
		Key:       key,
		Title:     a.Title,
		Author:    a.Author,
		FetchedAt: a.FetchedAt,
		Source:    a.Source,
	})
}

// loadIndex 读缓存索引；缺失返回空表。损坏时返回错误（调用方回退空表）。
func loadIndex() ([]indexEntry, error) {
	data, err := os.ReadFile(indexPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []indexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	sortIndex(entries)
	return entries, nil
}

// addIndexEntry 插入/更新条目并按 FetchedAt 倒序排序；超限时淘汰最旧条目及其文章文件。
func addIndexEntry(e indexEntry) error {
	entries, err := loadIndex()
	if err != nil {
		// 索引损坏：以空表重建，保住本次抓取。
		entries = nil
	}
	merged := make([]indexEntry, 0, len(entries)+1)
	for _, old := range entries {
		if old.Key != e.Key {
			merged = append(merged, old)
		}
	}
	merged = append(merged, e)
	sortIndex(merged)
	var evicted []indexEntry
	if len(merged) > maxCachedArticles {
		evicted = merged[maxCachedArticles:]
		merged = merged[:maxCachedArticles]
	}
	if err := atomicWrite(indexPath(), merged); err != nil {
		return err
	}
	for _, old := range evicted {
		_ = os.Remove(articlePath(old.Key))
	}
	return nil
}

func sortIndex(entries []indexEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].FetchedAt == entries[j].FetchedAt {
			return entries[i].Key > entries[j].Key
		}
		return entries[i].FetchedAt > entries[j].FetchedAt
	})
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
