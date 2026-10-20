// 视图渲染：首页 / 缓存列表 / 抓取中 / 阅读 / 设置 / 错误。
// 骨架移植自 c1weread / reader-lab。
package main

import (
	"fmt"
	"image"
	"strings"
	"time"

	"c1device"
)

const (
	rowH        = 16
	titleY      = 0
	contentY    = rowH
	listRows    = 7 // 列表行数（第 8 行留给状态）
	statusY     = contentY + listRows*rowH
	textWidthPx = 288
	homeItems   = 3 // 首页固定三个条目
)

// homeLabels 首页条目。
var homeLabels = []string{
	"今日一读",
	"随机一篇",
	"缓存列表",
}

// viewMode 视图状态机。
type viewMode int

const (
	viewHome viewMode = iota
	viewCache
	viewFetching
	viewReader
	viewSettings
	viewError
)

// render 按当前视图渲染整帧。
func (a *app) render() c1device.Frame {
	c := c1device.NewCanvas()
	c.Clear()
	switch a.view {
	case viewHome:
		a.renderHome(c)
	case viewCache:
		a.renderCache(c)
	case viewFetching:
		a.renderFetching(c)
	case viewReader:
		a.renderReader(c)
	case viewSettings:
		a.renderSettings(c)
	case viewError:
		c.DrawText(a.face, 2, titleY, "出错")
		drawPlainLines(c, a.face, wrapStatusLines(a.errMsg))
		c.DrawText(a.face, 2, statusY, "OK重试 ←/Back 返回")
	}
	return c.Frame(128)
}

func drawPlainLines(c *c1device.Canvas, face *c1device.Face, lines []string) {
	for i, line := range lines {
		if i >= listRows+1 {
			break
		}
		c.DrawText(face, 2, contentY+i*rowH, line)
	}
}

// renderHome 首页列表：今日一读 / 随机一篇 / 缓存列表。
// 骨架移植自 reader-lab app.go renderList。
func (a *app) renderHome(c *c1device.Canvas) {
	c.DrawText(a.face, 2, titleY, "每日一读")
	c.DrawTextRight(a.face, c1device.DisplayWidth-2, titleY, "dushu.com")
	c.FillRect(image.Rect(2, contentY-2, c1device.DisplayWidth-2, contentY))
	first, sel := a.homeFirst, a.homeSel
	for row := 0; row < listRows; row++ {
		idx := first + row
		if idx >= homeItems {
			break
		}
		top := contentY + row*rowH
		rect := image.Rect(0, top, c1device.DisplayWidth, top+rowH)
		label := " " + homeLabels[idx]
		label = truncateToWidth(a.face, label, c1device.DisplayWidth-4)
		if idx == sel {
			c.DrawInvertedTextBar(a.face, rect, label)
		} else {
			c.DrawText(a.face, 2, top, label)
		}
	}
	drawNavFooter(c, a.face, statusY, "OK打开", "↑↓选择", "Back退出")
}

// renderCache 缓存列表：之前读过的文章，按时间倒序。
func (a *app) renderCache(c *c1device.Canvas) {
	c.DrawText(a.face, 2, titleY, "缓存列表")
	c.DrawTextRight(a.face, c1device.DisplayWidth-2, titleY, fmt.Sprintf("%d 篇", len(a.cacheEntries)))
	c.FillRect(image.Rect(2, contentY-2, c1device.DisplayWidth-2, contentY))
	count := len(a.cacheEntries)
	if count == 0 {
		c.DrawTextCentered(a.face, c1device.DisplayWidth/2, contentY+3*rowH, "暂无缓存")
		c.DrawTextCentered(a.face, c1device.DisplayWidth/2, contentY+5*rowH, "读过的文章自动保存在这里")
	} else {
		first, sel := a.cacheFirst, a.cacheSel
		for row := 0; row < listRows; row++ {
			idx := first + row
			if idx >= count {
				break
			}
			top := contentY + row*rowH
			rect := image.Rect(0, top, c1device.DisplayWidth, top+rowH)
			e := a.cacheEntries[idx]
			label := " " + e.Title
			if e.Author != "" {
				label += " · " + e.Author
			}
			label = truncateToWidth(a.face, label, c1device.DisplayWidth-4)
			if idx == sel {
				c.DrawInvertedTextBar(a.face, rect, label)
			} else {
				c.DrawText(a.face, 2, top, label)
			}
		}
		c.DrawScrollIndicator(c1device.DisplayWidth-3, contentY, contentY+listRows*rowH, sel, count)
	}
	drawNavFooter(c, a.face, statusY, "←首页", "↑↓选择", "OK阅读")
}

// renderFetching 抓取中提示。
func (a *app) renderFetching(c *c1device.Canvas) {
	c.DrawText(a.face, 2, titleY, "抓取中")
	c.FillRect(image.Rect(2, contentY-2, c1device.DisplayWidth-2, contentY))
	lines := []string{
		"正在从读书网抓取文章…",
		"站点较慢，最长约 30 秒",
		"",
		"Back 取消",
	}
	drawPlainLines(c, a.face, lines)
}

// renderReader 正文阅读页。
// 普通模式：顶栏（标题·作者 / 页码 n/m + 分隔线）、正文、倒显底栏按键提示；
// 全屏模式只画正文。骨架移植自 reader-lab app.go renderReader。
func (a *app) renderReader(c *c1device.Canvas) {
	if a.fullscreen {
		a.renderReaderBody(c, a.readerBodyTop())
		return
	}
	pageCount := max(1, len(a.pages))
	pageLabel := fmt.Sprintf("%d/%d", a.readerPage+1, pageCount)
	pageW := a.face.Measure(pageLabel)
	pageX := c1device.DisplayWidth - 2 - pageW
	c.DrawText(a.face, pageX, titleY, pageLabel)
	c.DrawText(a.face, 2, titleY, truncateToWidth(a.face, a.readerTitle, pageX-2-6))
	c.FillRect(image.Rect(2, contentY-2, c1device.DisplayWidth-2, contentY))
	a.renderReaderBody(c, contentY)
	if a.message != "" {
		c.DrawInvertedTextBar(a.face, image.Rect(0, readerFooterY, c1device.DisplayWidth, c1device.DisplayHeight),
			truncateToWidth(a.face, a.message, c1device.DisplayWidth-4))
	} else {
		drawNavFooter(c, a.face, readerFooterY, "←返回", "↑↓翻页", "OK全屏", "S设置")
	}
}

// renderReaderBody 画当前页正文行（TTF 走阈值二值化），右侧画页面滚动条。
func (a *app) renderReaderBody(c *c1device.Canvas, top int) {
	if len(a.pages) == 0 {
		return
	}
	page := a.pages[a.readerPage]
	lineHeight := a.bodyFace.LineHeight()
	if lineHeight < 1 {
		lineHeight = rowH
	}
	for _, line := range page.Lines {
		if isTTF(a.settings.Font) {
			c.DrawTextThreshold(a.bodyFace, 4, top, line, uint8(a.settings.Threshold))
		} else {
			c.DrawText(a.bodyFace, 4, top, line)
		}
		top += lineHeight
	}
	pageCount := max(1, len(a.pages))
	if pageCount > 1 {
		bodyTop := a.readerBodyTop()
		c.DrawScrollIndicator(c1device.DisplayWidth-3, bodyTop, bodyTop+len(page.Lines)*lineHeight, a.readerPage, pageCount)
	}
}

// drawNavFooter 倒显底栏：按键提示按从左到右均匀铺开。
// 移植自 reader-lab navigation_ui.go drawNavigationFooter（四格提示的通用化）。
func drawNavFooter(c *c1device.Canvas, face *c1device.Face, top int, labels ...string) {
	bar := image.Rect(0, top, c1device.DisplayWidth, c1device.DisplayHeight)
	n := len(labels)
	if n == 0 {
		c.InvertRect(bar)
		return
	}
	slot := (c1device.DisplayWidth - 8) / n
	if slot < 8 {
		slot = 8
	}
	for i, label := range labels {
		c.DrawText(face, 4+i*slot, top+4, truncateToWidth(face, label, slot-4))
	}
	c.InvertRect(bar)
}

// renderSettings 阅读设置视图。
func (a *app) renderSettings(c *c1device.Canvas) {
	c.DrawText(a.face, 2, titleY, "阅读设置")
	fontLabel := "字体  " + fontDisplayName(a.settings)
	sizeLabel := fmt.Sprintf("字号  %d px", a.settings.Size)
	thresholdLabel := fmt.Sprintf("阈值  %d", a.settings.Threshold)
	if !isTTF(a.settings.Font) {
		thresholdLabel += " (点阵无影响)"
	}
	items := []string{fontLabel, sizeLabel, thresholdLabel}
	hint := "↑↓选择 ←→调整 S/Back 返回"
	if a.message != "" {
		hint = a.message
	}
	for i, item := range items {
		top := contentY + i*rowH
		if i == a.settingsSel {
			rect := image.Rect(0, top, c1device.DisplayWidth, top+rowH)
			c.DrawInvertedTextBar(a.face, rect, truncateToWidth(a.face, "› "+item, c1device.DisplayWidth-4))
		} else {
			c.DrawText(a.face, 2, top, truncateToWidth(a.face, "  "+item, c1device.DisplayWidth-4))
		}
	}
	c.DrawText(a.face, 2, statusY, truncateToWidth(a.face, hint, c1device.DisplayWidth-4))
}

// truncateToWidth 按像素宽截断文本（超宽加 …）。
func truncateToWidth(face *c1device.Face, s string, width int) string {
	if face.Measure(s) <= width {
		return s
	}
	for len(s) > 1 {
		s = s[:len(s)-1]
		if face.Measure(s+"…") <= width {
			return s + "…"
		}
	}
	return "…"
}

// wrapStatusLines 把错误信息按屏宽折行。
func wrapStatusLines(msg string) []string {
	var out []string
	for _, raw := range strings.Split(msg, "\n") {
		line := strings.TrimSpace(raw)
		for line != "" {
			runes := []rune(line)
			w := 0
			cut := 0
			for cut < len(runes) {
				rw := runeAdvance(runes[cut])
				if w+rw > textWidthPx-8 && cut > 0 {
					break
				}
				w += rw
				cut++
			}
			out = append(out, string(runes[:cut]))
			line = strings.TrimSpace(string(runes[cut:]))
		}
	}
	if len(out) == 0 {
		out = []string{"未知错误"}
	}
	return out
}

func runeAdvance(r rune) int {
	if r < 0x80 {
		return 8
	}
	return 16
}

// formatFetchedAt 把抓取时间戳格式化为短日期（缓存列表可用）。
func formatFetchedAt(ts int64) string {
	return time.Unix(ts, 0).Format("01-02")
}
