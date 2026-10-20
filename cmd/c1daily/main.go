// 每日一读 — C1-Slim 墨水屏每日美文客户端。
// 启动即读今日文章（有缓存秒开，否则联网抓取）；阅读器 R 随机一篇，
// Back 进缓存列表，再 Back 退出应用。
// UI/渲染/字体/分页栈移植自 c1weread / reader-lab。
package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"c1device"
)

//go:embed assets/pkg-font.bin
var fontData []byte

// fetchKind 抓取任务类型。
type fetchKind int

const (
	fetchToday fetchKind = iota
	fetchRandom
)

// fetchResult 后台抓取结果，经 fetchDone 汇报给主循环。
type fetchResult struct {
	kind    fetchKind
	key     string // 缓存 key（成功时填写）
	article *Article
	err     error
}

type app struct {
	face     *c1device.Face // UI 字体（内嵌 Unifont 16px）
	bodyFace *c1device.Face // 正文字体
	fontData *mappedFont    // TTF 运行时字体数据（Face 借用其 typeface）

	view    viewMode
	errMsg  string
	message string // 阅读器/设置等临时提示

	cacheEntries []indexEntry
	cacheSel     int
	cacheFirst   int

	article     *Article // 当前阅读的文章
	pages       []Page   // 当前文章分页结果
	readerPage  int
	fullscreen  bool // 阅读器全屏模式（会话级，不持久化）
	readerTitle string

	settings    ReaderSettings
	settingsSel int

	fetchDone   chan fetchResult // 后台抓取完成信号（nil = 空闲）
	retryKind   fetchKind        // 错误屏 OK 重试时重新发起的抓取类型
	fetchCancel bool             // 抓取已被 Back 取消，结果到来时忽略
}

// writeTrace 向数据目录的 c1daily.trace 追加一行（崩溃/信号诊断用，与启动器日志无关）。
func writeTrace(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(dataDir(), "c1daily.trace"),
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
		c1device.ReturnToDesktop()
		os.Exit(0)
	}()
	// 正常退出路径（Back 返回桌面）：local-apps 启动器在子进程退出后自行恢复桌面；
	// terminal 启动模式（C1_C1ANCHER_TERMINAL=1）下需要主动结束 C1ancher 终端页。
	defer c1device.ReturnToDesktop()

	face, err := c1device.NewBitmapFace(fontData, 16)
	if err != nil {
		fmt.Fprintln(os.Stderr, "c1daily: ui font:", err)
		os.Exit(1)
	}
	defer face.Close()

	if err := ensureDataDir(); err != nil {
		fmt.Fprintln(os.Stderr, "c1daily: data dir:", err)
		os.Exit(1)
	}

	settings, settingsErr := loadReaderSettings()
	bodyFace, fontData, err := newReaderFace(settings)
	if err != nil {
		if settingsErr == nil {
			fmt.Fprintf(os.Stderr, "c1daily: body font (%s): %v; fallback unifont\n", settings.Font, err)
		}
		settings = defaultReaderSettings()
		bodyFace, fontData, err = newReaderFace(settings)
		if err != nil {
			fmt.Fprintln(os.Stderr, "c1daily: fallback font:", err)
			os.Exit(1)
		}
	}
	defer bodyFace.Close()
	if fontData != nil {
		defer fontData.Close()
	}

	a := &app{
		face:     face,
		bodyFace: bodyFace,
		fontData: fontData,
		settings: settings,
		view:     viewCache,
	}

	// 启动即读：有今日缓存秒开，否则联网抓取（抓取中视图可 Back 取消）。
	a.refreshCacheEntries()
	a.openToday()

	platform, err := c1device.OpenPlatform()
	if err != nil {
		fmt.Fprintln(os.Stderr, "c1daily: open platform:", err)
		os.Exit(1)
	}
	defer platform.Close()

	// 首帧全刷
	_ = platform.Draw(a.render(), true)

	for {
		select {
		case ev, ok := <-platform.Events():
			if !ok {
				return
			}
			// 音量键映射为方向键（同 reader-lab / c1weread）：音量+ = ↓，音量- = ↑。
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
			if a.handleEvent(ev) {
				return
			}
		case res := <-a.fetchDone:
			// 后台抓取结束：统一在主 goroutine 里切换视图
			a.fetchDone = nil
			if a.fetchCancel {
				// 用户已按 Back 取消并进入错误屏，丢弃迟到结果
				a.fetchCancel = false
				break
			}
			if res.err != nil {
				a.retryKind = res.kind
				writeTrace("fetch %d failed: %v", res.kind, res.err)
				a.fail(fmt.Sprintf("抓取失败: %v\n\nOK 重试，←/Back 返回缓存列表", res.err))
			} else {
				if err := cacheArticle(res.key, res.article); err != nil {
					writeTrace("cache article %s: %v", res.key, err)
				}
				a.refreshCacheEntries()
				a.openArticle(res.article)
			}
		}
		_ = platform.Draw(a.render(), false)
	}
}

// refreshCacheEntries 重新加载缓存索引。
func (a *app) refreshCacheEntries() {
	entries, err := loadIndex()
	if err != nil {
		writeTrace("load index: %v", err)
		entries = nil
	}
	a.cacheEntries = entries
}

// handleEvent 分发按键；返回 true 表示退出。
func (a *app) handleEvent(ev c1device.Event) bool {
	a.message = "" // 临时提示只存活到下一次按键（reader-lab 同款语义）
	switch a.view {
	case viewCache:
		return a.handleCache(ev)
	case viewReader:
		return a.handleReader(ev)
	case viewSettings:
		return a.handleSettings(ev)
	case viewError:
		switch ev.Key {
		case c1device.KeyOK:
			a.startFetch(a.retryKind)
		case c1device.KeyLeft, c1device.KeyBack:
			a.view = viewCache
		}
	case viewFetching:
		// 抓取中：Back 取消（后台 goroutine 结果到来时按 fetchCancel 丢弃）
		if ev.Key == c1device.KeyBack {
			a.fetchCancel = true
			a.fail("已取消抓取\n\n←/Back 返回缓存列表")
		}
	}
	return false
}

// openToday 打开今日文章：本地有今日缓存直接读（离线可读），否则联网抓取。
func (a *app) openToday() {
	key := todayKey(time.Now())
	if article, err := loadArticle(key); err == nil && article != nil {
		a.openArticle(article)
		return
	} else if err != nil {
		writeTrace("load today cache %s: %v", key, err)
	}
	a.startFetch(fetchToday)
}

// openCache 从缓存列表打开选中的文章。
func (a *app) openCache() {
	if a.cacheSel < 0 || a.cacheSel >= len(a.cacheEntries) {
		return
	}
	key := a.cacheEntries[a.cacheSel].Key
	article, err := loadArticle(key)
	if err != nil || article == nil {
		a.fail(fmt.Sprintf("读取缓存失败: %v\n\n←/Back 返回缓存列表", err))
		return
	}
	a.openArticle(article)
}

// startFetch 后台抓取（today/random）；完成后直接进入阅读器，Back 退回缓存列表。
func (a *app) startFetch(kind fetchKind) {
	a.fetchCancel = false
	a.view = viewFetching
	a.fetchDone = make(chan fetchResult, 1)
	go func() {
		// panic 防护：抓取在后台 goroutine 跑，崩了要留痕并回报失败而不是闪退。
		defer func() {
			if r := recover(); r != nil {
				writeTrace("fetch panic: %v\n%s", r, debug.Stack())
				a.fetchDone <- fetchResult{kind: kind, err: fmt.Errorf("internal error: %v", r)}
			}
		}()
		var (
			article *Article
			err     error
		)
		switch kind {
		case fetchToday:
			article, err = FetchToday()
		default:
			article, err = FetchRandom()
		}
		res := fetchResult{kind: kind, article: article, err: err}
		if err == nil {
			switch kind {
			case fetchToday:
				res.key = todayKey(time.Now())
			default:
				res.key = randomKey(article)
			}
		}
		a.fetchDone <- res
	}()
}

// openArticle 打开文章并分页进入阅读器。
func (a *app) openArticle(article *Article) {
	title := article.Title
	if article.Author != "" {
		title += "·" + article.Author
	}
	text := articleText(article)
	pages, err := paginate(text, a.bodyFace, a.readerBodyHeight())
	if err != nil {
		a.fail(fmt.Sprintf("分页失败: %v\n\n←/Back 返回缓存列表", err))
		return
	}
	if len(pages) == 0 {
		pages = []Page{{Start: 0, End: len(text)}}
	}
	a.article = article
	a.readerTitle = title
	a.pages = pages
	a.readerPage = 0
	a.view = viewReader
}

// articleText 段落拼成带缩进的正文文本。
func articleText(a *Article) string {
	var text string
	for _, p := range a.Paragraphs {
		text += "　　" + p + "\n"
	}
	return text
}

// handleCache 缓存列表按键；Back 退出应用。
func (a *app) handleCache(ev c1device.Event) bool {
	n := len(a.cacheEntries)
	switch ev.Key {
	case c1device.KeyUp:
		if n > 0 && a.cacheSel > 0 {
			a.cacheSel--
			if a.cacheSel < a.cacheFirst {
				a.cacheFirst = a.cacheSel
			}
		}
	case c1device.KeyDown:
		if n > 0 && a.cacheSel < n-1 {
			a.cacheSel++
			if a.cacheSel >= a.cacheFirst+listRows {
				a.cacheFirst = a.cacheSel - listRows + 1
			}
		}
	case c1device.KeyOK:
		a.openCache()
	case c1device.KeyBack:
		return true
	}
	return false
}

// handleReader 阅读器按键。
func (a *app) handleReader(ev c1device.Event) bool {
	switch ev.Key {
	case c1device.KeyUp:
		if a.readerPage > 0 {
			a.readerPage--
		}
	case c1device.KeyDown:
		if a.readerPage < len(a.pages)-1 {
			a.readerPage++
		}
	case c1device.KeyOK:
		a.toggleFullscreen()
	case c1device.KeyLeft, c1device.KeyBack:
		a.view = viewCache
	case c1device.KeyRune:
		switch ev.Rune {
		case 's', 'S':
			a.view = viewSettings
			a.settingsSel = 0
		case 'f', 'F':
			a.toggleFullscreen()
		case 'r', 'R':
			a.startFetch(fetchRandom)
		}
	}
	return false
}

// toggleFullscreen 切换阅读器全屏模式，重分页并保持当前页首字节偏移。
func (a *app) toggleFullscreen() {
	if a.view != viewReader || len(a.pages) == 0 {
		return
	}
	a.fullscreen = !a.fullscreen
	a.repageCurrentArticle()
}

// readerBodyTop 返回正文区顶部 y 坐标（全屏留 4px 边距，同 reader-lab fullscreenBodyTop）。
func (a *app) readerBodyTop() int {
	if a.fullscreen {
		return 4
	}
	return rowH
}

// readerBodyHeight 返回当前正文区高度（全屏时为整屏减边距）。
func (a *app) readerBodyHeight() int {
	if a.fullscreen {
		return c1device.DisplayHeight - 2*4
	}
	return readerBodyHeight
}

// repageCurrentArticle 重新分页并尽量保持当前页首字节偏移。
func (a *app) repageCurrentArticle() {
	if a.article == nil {
		return
	}
	offset := 0
	if a.readerPage >= 0 && a.readerPage < len(a.pages) {
		offset = a.pages[a.readerPage].Start
	}
	text := articleText(a.article)
	pages, err := paginate(text, a.bodyFace, a.readerBodyHeight())
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
		if a.settingsSel < 2 {
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
				a.message = "Fusion fixed 12px"
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
				a.message = sanitizeMessage(err.Error()) + " (fallback unifont)"
				settings = ReaderSettings{Font: fontUnifont, Size: 16, Threshold: settings.Threshold}
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

	if a.view == viewReader && a.article != nil {
		var offset int
		if a.readerPage >= 0 && a.readerPage < len(a.pages) {
			offset = a.pages[a.readerPage].Start
		}
		text := articleText(a.article)
		pages, perr := paginate(text, a.bodyFace, a.readerBodyHeight())
		if perr != nil {
			a.bodyFace, a.fontData, a.settings = oldFace, oldData, oldSettings
			if needFace {
				face.Close()
				if data != nil {
					data.Close()
				}
			}
			return fmt.Errorf("re-paginate: %w", perr)
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
			return fmt.Errorf("save settings: %w", err)
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
