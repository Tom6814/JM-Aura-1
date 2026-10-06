package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"jmaura/internal/bika"
)

// bikaImageClient 下载第三方图源图片：哔咔 CDN 证书链常不完整，故免校验 + 单请求超时。
var bikaImageClient = func() *http.Client {
	c := newProxyClient(false)
	c.Timeout = 60 * time.Second
	return c
}()

func bikaDefaultStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// bikaImageExt 从图片 URL 推断扩展名（默认 .jpg）。
func bikaImageExt(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		base := path.Base(u.Path)
		if i := strings.LastIndex(base, "."); i >= 0 && i < len(base)-1 {
			if ext := base[i:]; len(ext) <= 6 {
				return ext
			}
		}
	}
	return ".jpg"
}

// bikaChapterImageList 拉取某章节的全部图片直链（分页）。
func bikaChapterImageList(ctx context.Context, identity, comicID string, order int) ([]string, error) {
	var urls []string
	for page := 1; page <= 200; page++ {
		p := page
		data, err := bikaCall(ctx, identity, func(tok string) (any, error) {
			return bikaClient.ChapterPages(ctx, tok, comicID, order, p)
		})
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		pd := bika.AsMap(bika.AsMap(data)["pages"])
		for _, d := range bika.AsList(pd["docs"]) {
			media := bika.AsMap(bika.AsMap(d)["media"])
			raw := bika.ImageURL(bika.AsStr(media["fileServer"]), bika.AsStr(media["path"]), bika.PictureComic)
			if raw != "" {
				urls = append(urls, raw)
			}
		}
		total := bika.AsInt(pd["pages"])
		if total <= 1 || page >= total {
			break
		}
	}
	return urls, nil
}

// bikaDownloadOne 下载单张图片到 dest。
func bikaDownloadOne(rawURL, dest string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	if u, perr := url.Parse(rawURL); perr == nil {
		req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")
	}
	req.Header.Set("User-Agent", chrome120UA)
	resp, err := bikaImageClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bika: 图片下载失败(%d)", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, resp.Body)
	return err
}

// executeBika 执行哔咔源下载任务：图片为直链、无需置乱解码，落盘后复用通用打包收尾。
func (m *downloadTaskManager) executeBika(t *downloadTask) error {
	taskID := t.TaskID
	taskDir := filepath.Join(m.baseDir, taskID)
	workDir := filepath.Join(taskDir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	albumFolder := t.AlbumID
	if t.AlbumTitle != "" {
		albumFolder = dlSafeName(t.AlbumTitle, 80)
	}
	rootOut := filepath.Join(workDir, albumFolder)
	if err := os.MkdirAll(rootOut, 0o755); err != nil {
		return err
	}
	if len(t.Chapters) == 0 {
		return fmt.Errorf("No chapters selected")
	}

	m.update(taskID, func(t *downloadTask) {
		t.Status = "downloading"
		t.Stage = "downloading"
		t.Message = "Downloading..."
		t.DownloadedImages = 0
		t.TotalImages = 0
		t.Percent = 0.0
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	identity := t.identity
	downloaded := 0
	totalKnown := 0
	for _, ch := range t.Chapters {
		comicID, order, ok := bikaSplitChapterKey(ch.ID)
		if !ok {
			continue
		}
		chDir := filepath.Join(rootOut, dlSafeName(bikaDefaultStr(ch.Title, ch.ID), 80))
		if err := os.MkdirAll(chDir, 0o755); err != nil {
			return err
		}
		pages, err := bikaChapterImageList(ctx, identity, comicID, order)
		if err != nil {
			return err
		}
		totalKnown += len(pages)
		total := totalKnown
		m.update(taskID, func(t *downloadTask) { t.TotalImages = total })
		m.recompute(taskID)
		for i, rawURL := range pages {
			dest := filepath.Join(chDir, fmt.Sprintf("%03d%s", i+1, bikaImageExt(rawURL)))
			if derr := bikaDownloadOne(rawURL, dest); derr != nil {
				return derr
			}
			downloaded++
			d := downloaded
			m.update(taskID, func(t *downloadTask) { t.DownloadedImages = d })
			m.recompute(taskID)
		}
	}
	if downloaded == 0 {
		return fmt.Errorf("No images found for selected chapters")
	}
	return dlFinalizeZip(m, taskID, t, taskDir, workDir, rootOut)
}
