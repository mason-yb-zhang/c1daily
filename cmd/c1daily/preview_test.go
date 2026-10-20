package main

import (
	"os"
	"path/filepath"
	"testing"

	"c1device"
)

// TestPreview 渲染各视图到 .frame 供 PC 侧转 PNG 检查。
func TestPreview(t *testing.T) {
	face, err := c1device.NewBitmapFace(fontData, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()

	article := &Article{
		Title:      "Preview Article Title",
		Author:     "Some Author",
		Paragraphs: []string{"First paragraph of the preview article, long enough to wrap across the narrow e-paper screen width.", "Second paragraph body text for preview rendering."},
		FetchedAt:  1729430400,
		Source:     dushuSourceToday,
	}
	readerText := articleText(article)
	readerPages, err := paginate(readerText, face, readerBodyHeight)
	if err != nil {
		t.Fatal(err)
	}
	fullscreenPages, err := paginate(readerText, face, c1device.DisplayHeight-2*4)
	if err != nil {
		t.Fatal(err)
	}
	if len(readerPages) == 0 {
		readerPages = []Page{{Start: 0, End: len(readerText)}}
	}
	if len(fullscreenPages) == 0 {
		fullscreenPages = readerPages
	}
	readerTitle := article.Title + "·" + article.Author

	cases := []struct {
		name string
		app  *app
	}{
		{"cache", &app{
			face: face,
			view: viewCache,
			cacheEntries: []indexEntry{
				{Key: "2026-10-21", Title: "Cached Article One", Author: "Author A", FetchedAt: 1729430400, Source: dushuSourceToday},
				{Key: "abc123def4567890", Title: "Cached Article Two With A Longer Title", Author: "Author B", FetchedAt: 1729344000, Source: dushuSourceRandom},
			},
			cacheSel:   1,
			cacheFirst: 0,
		}},
		{"cache-empty", &app{
			face: face,
			view: viewCache,
		}},
		{"fetching", &app{
			face: face,
			view: viewFetching,
		}},
		{"reader", &app{
			face:        face,
			bodyFace:    face,
			view:        viewReader,
			article:     article,
			readerTitle: readerTitle,
			pages:       readerPages,
			readerPage:  0,
			settings:    ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 128},
		}},
		{"reader-fullscreen", &app{
			face:        face,
			bodyFace:    face,
			view:        viewReader,
			fullscreen:  true,
			article:     article,
			readerTitle: readerTitle,
			pages:       fullscreenPages,
			readerPage:  0,
			settings:    ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 128},
		}},
		{"settings", &app{
			face:        face,
			bodyFace:    face,
			view:        viewSettings,
			article:     article,
			settings:    ReaderSettings{Font: fontUnifont, Size: 16, Threshold: 128},
			settingsSel: 1,
		}},
		{"error", &app{
			face:   face,
			view:   viewError,
			errMsg: "fetch dushu-today: http timeout\n\nOK retry, Back back",
		}},
	}

	// 若运行时字体目录存在对应 TTF，则额外渲染 14px 阅读视图；缺失时跳过，测试不失败。
	for _, fc := range []struct {
		name     string
		settings ReaderSettings
	}{
		{"reader-lxgw", ReaderSettings{Font: fontLXGW, Size: 14, Threshold: 128}},
		{"reader-misans", ReaderSettings{Font: fontMiSans, Size: 14, Threshold: 128}},
	} {
		bodyFace, fontData, err := newReaderFace(fc.settings)
		if err != nil {
			t.Logf("%s: font unavailable, skip: %v", fc.name, err)
			continue
		}
		pages, err := paginate(readerText, bodyFace, readerBodyHeight)
		if err != nil {
			bodyFace.Close()
			if fontData != nil {
				fontData.Close()
			}
			t.Fatal(err)
		}
		if len(pages) == 0 {
			pages = []Page{{Start: 0, End: len(readerText)}}
		}
		cases = append(cases, struct {
			name string
			app  *app
		}{
			fc.name,
			&app{
				face:        face,
				bodyFace:    bodyFace,
				fontData:    fontData,
				view:        viewReader,
				article:     article,
				readerTitle: readerTitle,
				pages:       pages,
				readerPage:  0,
				settings:    fc.settings,
			},
		})
	}

	outDir := filepath.Join("..", "..", "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		frame := tc.app.render()
		if len(frame) != c1device.FrameBytes {
			t.Errorf("%s: frame = %d bytes, want %d", tc.name, len(frame), c1device.FrameBytes)
		}
		path := filepath.Join(outDir, "preview-"+tc.name+".frame")
		if err := os.WriteFile(path, frame[:], 0o644); err != nil {
			t.Fatal(err)
		}
		if tc.app.fontData != nil {
			tc.app.fontData.Close()
		}
		if tc.app.bodyFace != nil && tc.app.bodyFace != face {
			tc.app.bodyFace.Close()
		}
	}
}
