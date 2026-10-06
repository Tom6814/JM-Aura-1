package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/webp"

	"jmaura/internal/jm"
	"jmaura/internal/store"
)

var reUnsafeChars = regexp.MustCompile(`[<>:"/\\|?*]+`)

func dlSafeName(name string, maxLen int) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return "untitled"
	}
	s = reUnsafeChars.ReplaceAllString(s, "_")
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > maxLen {
		return string(r[:maxLen])
	}
	return s
}

func dlSegmentationNum(epsID, scrambleID int, pictureName string) int {
	if epsID < scrambleID {
		return 0
	}
	if epsID < 268850 {
		return 10
	}
	sum := md5.Sum([]byte(strconv.Itoa(epsID) + pictureName))
	hexStr := hex.EncodeToString(sum[:])
	keyCode := int(hexStr[len(hexStr)-1])
	if epsID > 421926 {
		return (keyCode%8)*2 + 2
	}
	return (keyCode%10)*2 + 2
}

// dlJpegQuality 是默认（非无损）模式下重编码 JPEG 的质量：体积小、与上游观感一致。
const dlJpegQuality = 75

func dlDecodeImageBytes(imgBytes []byte, epsID, scrambleID int, pictureName, origName string, isGIF bool, lossless bool) ([]byte, string) {
	origExt := strings.ToLower(filepath.Ext(origName))
	if origExt == "" {
		origExt = ".jpg"
	}
	if isGIF {
		return imgBytes, origExt
	}
	num := dlSegmentationNum(epsID, scrambleID, pictureName)
	if num <= 1 {
		return imgBytes, origExt
	}
	// lossless=true：反置乱后以无损 PNG 落盘（零额外损失，体积大）；
	// lossless=false：沿用 JPEG(dlJpegQuality) 重编码（体积小，默认）。
	out, ct, ok := descrambleImageBytes(imgBytes, num, lossless, dlJpegQuality)
	if !ok {
		return imgBytes, origExt
	}
	if ct == "image/png" {
		return out, ".png"
	}
	return out, ".jpg"
}

// descrambleImageBytes 把图片按 num 段做「末段在前」的反置乱重排并重编码。
// num <= 1、格式不受支持或解码失败时返回 ok=false，调用方应回退原始字节。
// 分块口径必须与前端旧实现及上游完全一致。
//
// forcePNG=true 时一律无损 PNG 输出（下载归档，100% 无额外损失）；
// 否则源为 PNG 时无损输出、其余按 jpegQuality 重编码（在线阅读，兼顾体积与速度）。
func descrambleImageBytes(imgBytes []byte, num int, forcePNG bool, jpegQuality int) ([]byte, string, bool) {
	if num <= 1 {
		return nil, "", false
	}
	ct := http.DetectContentType(imgBytes)
	var src image.Image
	srcPNG := false
	switch ct {
	case "image/jpeg":
		im, err := jpeg.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			return nil, "", false
		}
		src = im
	case "image/png":
		im, err := png.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			return nil, "", false
		}
		src, srcPNG = im, true
	case "image/webp":
		im, err := webp.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			return nil, "", false
		}
		src = webpDisplayRGB(im)
	default:
		return nil, "", false
	}

	b := src.Bounds()
	width, height := b.Dx(), b.Dy()
	if width <= 0 || height <= 0 {
		return nil, "", false
	}
	des := image.NewRGBA(image.Rect(0, 0, width, height))

	// 与旧版一致：各块高度为 copyHeight 累加，余数并入最后一块，再从最后一块向前依次绘制
	rem := height % num
	copyHeight := height / num
	destY := 0
	for i := num - 1; i >= 0; i-- {
		start := copyHeight * i
		end := copyHeight * (i + 1)
		if i == num-1 {
			end += rem
		}
		sliceH := end - start
		draw.Draw(des, image.Rect(0, destY, width, destY+sliceH), src, image.Pt(b.Min.X, b.Min.Y+start), draw.Src)
		destY += sliceH
	}

	var out bytes.Buffer
	if forcePNG || srcPNG {
		if err := png.Encode(&out, des); err != nil {
			return nil, "", false
		}
		return out.Bytes(), "image/png", true
	}
	if err := jpeg.Encode(&out, des, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, "", false
	}
	return out.Bytes(), "image/jpeg", true
}

func dlCandidateHosts(domain string) []string {
	out := []string{}
	if d := strings.TrimSpace(domain); d != "" {
		d = strings.ReplaceAll(d, "https://", "")
		d = strings.ReplaceAll(d, "http://", "")
		d = strings.Trim(d, "/")
		if d != "" {
			out = append(out, d)
		}
	}
	for _, u := range apiClient().ImageDomains() {
		host := strings.TrimSpace(u)
		if host == "" {
			continue
		}
		if parsed, perr := url.Parse(host); perr == nil && parsed.Host != "" {
			host = parsed.Host
		}
		out = append(out, host)
	}
	out = append(out, "cdn-msp.jmapinodeudzn.net")
	seen := map[string]bool{}
	uniq := make([]string, 0, len(out))
	for _, h := range out {
		if seen[h] {
			continue
		}
		seen[h] = true
		uniq = append(uniq, h)
	}
	return uniq
}

func dlDownloadOneImage(ctx context.Context, photoID, imageName, domain string) ([]byte, string, error) {
	client := proxyClientNoVerify
	lastErr := fmt.Errorf("no candidate host")
	for _, host := range dlCandidateHosts(domain) {
		u := fmt.Sprintf("https://%s/media/photos/%s/%s", host, photoID, imageName)
		actx, cancel := context.WithTimeout(ctx, 25*time.Second)
		req, rerr := http.NewRequestWithContext(actx, http.MethodGet, u, nil)
		if rerr != nil {
			cancel()
			lastErr = rerr
			continue
		}
		req.Header.Set("User-Agent", chrome120UA)
		req.Header.Set("Referer", "https://"+host+"/")
		resp, derr := client.Do(req)
		if derr != nil {
			cancel()
			lastErr = derr
			continue
		}
		data, rderr := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		if rderr != nil {
			lastErr = rderr
			continue
		}
		if resp.StatusCode == http.StatusOK && len(data) > 0 {
			return data, host, nil
		}
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil, "", fmt.Errorf("Image download failed: %s/%s (%v)", photoID, imageName, lastErr)
}

type downloadChapter struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type downloadTask struct {
	TaskID           string            `json:"task_id"`
	AlbumID          string            `json:"album_id"`
	AlbumTitle       string            `json:"album_title"`
	Chapters         []downloadChapter `json:"chapters"`
	Status           string            `json:"status"`
	Stage            string            `json:"stage"`
	Message          string            `json:"message"`
	CreatedAt        time.Time         `json:"-"`
	UpdatedAt        time.Time         `json:"-"`
	TotalImages      int               `json:"total_images"`
	DownloadedImages int               `json:"downloaded_images"`
	ZippedFiles      int               `json:"zipped_files"`
	TotalZipFiles    int               `json:"total_zip_files"`
	Percent          float64           `json:"percent"`
	ZipPath          string            `json:"-"`
	Source           string            `json:"-"`
	Lossless         bool              `json:"-"`

	identity string
	// owner 是创建任务时的站点用户名；下载功能限登录用户，用于校验任务归属。
	owner string
}

func (t *downloadTask) toPublic() map[string]any {
	downloadURL := ""
	if t.Status == "completed" && t.ZipPath != "" {
		// 仅保留 v2 下载链接（旧 /api/download/tasks 路由已移除）。
		downloadURL = fmt.Sprintf("/api/v2/%s/download/tasks/%s/download", t.Source, t.TaskID)
	}
	return map[string]any{
		"task_id":           t.TaskID,
		"album_id":          t.AlbumID,
		"album_title":       t.AlbumTitle,
		"status":            t.Status,
		"stage":             t.Stage,
		"message":           t.Message,
		"total_images":      t.TotalImages,
		"downloaded_images": t.DownloadedImages,
		"total_zip_files":   t.TotalZipFiles,
		"zipped_files":      t.ZippedFiles,
		"percent":           math.Round(t.Percent*10000) / 10000,
		"download_url":      downloadURL,
		"lossless":          t.Lossless,
	}
}

func newTaskUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type downloadTaskManager struct {
	baseDir string
	mu      sync.Mutex
	// cond 配合 mu 使用：pending 有任务时唤醒等待中的 worker。
	cond    *sync.Cond
	tasks   map[string]*downloadTask
	pending []string
	// conc 为同时执行的任务数（默认 1）。
	conc int
	// maxQueued 为单个站点账号允许同时排队的任务数（默认 10）。
	maxQueued int
}

// errDlQueueFull 表示该站点账号排队中的下载任务已达上限。
var errDlQueueFull = errors.New("download queue limit reached for this account")

// dlConcurrency 读取下载并发上限。
//
// 默认 1：这台机器只有 2 核，下载解码与图片代理抢同一批核，串行执行可保证
// 浏览体验不会被下载挤占。需要放宽时用 JM_AURA_DL_CONCURRENCY 覆盖（上限 4）。
func dlConcurrency() int {
	n := 1
	if v := strings.TrimSpace(os.Getenv("JM_AURA_DL_CONCURRENCY")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > 4 {
		n = 4
	}
	return n
}

func newDownloadTaskManager(baseDir string) *downloadTaskManager {
	_ = os.MkdirAll(baseDir, 0o755)
	m := &downloadTaskManager{
		baseDir:   baseDir,
		tasks:     make(map[string]*downloadTask),
		conc:      dlConcurrency(),
		maxQueued: dlMaxQueuedPerOwner(),
	}
	m.cond = sync.NewCond(&m.mu)
	for i := 0; i < m.conc; i++ {
		go m.run()
	}
	return m
}

// dlMaxQueuedPerOwner 限制单个站点账号「排队中」的任务数，默认 10，可用
// JM_AURA_DL_MAX_QUEUED 覆盖。这是防误刷的上限而非永久拒绝：任务被 worker 取走后
// 该账号即可继续提交，所以最终都能下载完成。
func dlMaxQueuedPerOwner() int {
	n := 10
	if v := strings.TrimSpace(os.Getenv("JM_AURA_DL_MAX_QUEUED")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	return n
}

// queuedByOwnerLocked 统计 owner 仍在排队（status=queued）的任务数，须在 m.mu 内调用。
func (m *downloadTaskManager) queuedByOwnerLocked(owner string) int {
	n := 0
	for _, t := range m.tasks {
		if t.owner == owner && t.Status == "queued" {
			n++
		}
	}
	return n
}

// enqueue 把任务追加到待执行队列：永不阻塞、永不丢弃（队列只受内存限制）。
func (m *downloadTaskManager) enqueue(id string) {
	m.mu.Lock()
	m.pending = append(m.pending, id)
	m.mu.Unlock()
	m.cond.Signal()
}

// ownedSnapshot 在锁内取回任务的公开快照；仅当任务归属 owner 时才返回 found=true。
// 排队中的任务会附带 queue_position / queued_ahead，便于前端展示「前面还有几个」。
func (m *downloadTaskManager) ownedSnapshot(id, owner string) (pub map[string]any, status, zipPath string, found bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok || t.owner != owner {
		return nil, "", "", false
	}
	pub = t.toPublic()
	status = t.Status
	zipPath = t.ZipPath
	if status == "queued" {
		for i, pid := range m.pending {
			if pid == id {
				pub["queue_position"] = i + 1
				pub["queued_ahead"] = i
				break
			}
		}
	}
	return pub, status, zipPath, true
}

func (m *downloadTaskManager) createTask(albumID, albumTitle string, chapters []downloadChapter, identity, owner string, lossless bool) (*downloadTask, error) {
	return m.createTaskSource(albumID, albumTitle, chapters, identity, "jm", owner, lossless)
}

// createTaskSource 与 createTask 相同，但显式指定内容源（用于接入 bika 等新源）。
// 当 owner 排队中的任务已达上限时返回 errDlQueueFull，不创建任务。
func (m *downloadTaskManager) createTaskSource(albumID, albumTitle string, chapters []downloadChapter, identity, source, owner string, lossless bool) (*downloadTask, error) {
	m.mu.Lock()
	if m.queuedByOwnerLocked(owner) >= m.maxQueued {
		m.mu.Unlock()
		return nil, errDlQueueFull
	}
	t := &downloadTask{
		TaskID:     newTaskUUID(),
		AlbumID:    albumID,
		AlbumTitle: albumTitle,
		Chapters:   chapters,
		Status:     "queued",
		Stage:      "queued",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		identity:   identity,
		owner:      owner,
		Source:     source,
		Lossless:   lossless,
	}
	m.tasks[t.TaskID] = t
	m.mu.Unlock()
	m.enqueue(t.TaskID)
	return t, nil
}

// ownedSnapshot 见上（取代 getOwned/publicTask：一次锁内同时完成归属校验与快照）。

func (m *downloadTaskManager) update(id string, fn func(*downloadTask)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		fn(t)
		t.UpdatedAt = time.Now()
	}
}

func (m *downloadTaskManager) calcPercent(t *downloadTask) float64 {
	switch {
	case t.Status == "completed":
		return 1.0
	case t.Status == "failed":
		return t.Percent
	case t.Stage == "zipping":
		if t.TotalZipFiles > 0 {
			r := float64(t.ZippedFiles) / float64(t.TotalZipFiles)
			if r > 1 {
				r = 1
			}
			return 0.9 + 0.1*r
		}
		return 0.9
	case t.Stage == "downloading":
		if t.TotalImages > 0 {
			r := float64(t.DownloadedImages) / float64(t.TotalImages)
			if r > 1 {
				r = 1
			}
			return 0.9 * r
		}
		return 0.0
	}
	return 0.0
}

func (m *downloadTaskManager) recompute(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		t.Percent = m.calcPercent(t)
	}
}

func (m *downloadTaskManager) cancelQueued(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok || t.Status != "queued" {
		return false
	}
	t.Status = "failed"
	t.Stage = "cancelled"
	t.Message = "Cancelled"
	t.UpdatedAt = time.Now()
	// 同步移出待执行队列，避免排队位置统计把已取消任务算在内。
	for i, pid := range m.pending {
		if pid == id {
			m.pending = append(m.pending[:i], m.pending[i+1:]...)
			break
		}
	}
	return true
}

// isActive 报告任务是否仍在执行中（自动清理时跳过，避免误删进行中的下载）
func (m *downloadTaskManager) isActive(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return false
	}
	return t.Status == "queued" || t.Status == "downloading" || t.Status == "zipping"
}

func (m *downloadTaskManager) run() {
	for {
		m.mu.Lock()
		for len(m.pending) == 0 {
			m.cond.Wait()
		}
		id := m.pending[0]
		m.pending = m.pending[1:]
		t, ok := m.tasks[id]
		m.mu.Unlock()
		if !ok || t.Status != "queued" {
			continue
		}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					m.update(id, func(t *downloadTask) {
						t.Status = "failed"
						t.Stage = "failed"
						t.Message = fmt.Sprintf("%v", rec)
					})
				}
				m.recompute(id)
			}()
			if err := m.execute(t); err != nil {
				m.update(id, func(t *downloadTask) {
					t.Status = "failed"
					t.Stage = "failed"
					t.Message = err.Error()
				})
			}
		}()
	}
}

type dlChapterMeta struct {
	photoID    string
	title      string
	domain     string
	scrambleID int
	names      []string
}

func dlCollectChapterMetas(chapters []downloadChapter, ck map[string]string) ([]dlChapterMeta, int, error) {
	metas := make([]dlChapterMeta, 0, len(chapters))
	total := 0
	for _, c := range chapters {
		pid := strings.TrimSpace(c.ID)
		title := strings.TrimSpace(c.Title)
		if title == "" {
			title = pid
		}
		if pid == "" {
			continue
		}
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		info, err := fetchJmChapter(cctx, ck, pid)
		ccancel()
		if err != nil {
			return nil, 0, err
		}
		sc := atoiDefault(info.ScrambleID, int(jm.DefaultScrambleID))
		if sc <= 0 {
			sc = int(jm.DefaultScrambleID)
		}
		meta := dlChapterMeta{photoID: pid, title: title, domain: pyStr(info.DataOriginalDomain), scrambleID: sc, names: info.Names}
		metas = append(metas, meta)
		total += len(info.Names)
	}
	return metas, total, nil
}

func dlSaveChapterImages(rootOut string, meta dlChapterMeta, lossless bool, onImage func()) error {
	chapterFolder := filepath.Join(rootOut, dlSafeName(meta.title, 80))
	if err := os.MkdirAll(chapterFolder, 0o755); err != nil {
		return err
	}
	epsID, aerr := strconv.Atoi(strings.TrimSpace(meta.photoID))
	if aerr != nil {
		return aerr
	}
	for _, imgName := range meta.names {
		isGIF := strings.HasSuffix(strings.ToLower(imgName), ".gif")
		rawBytes, _, derr := dlDownloadOneImage(context.Background(), meta.photoID, imgName, meta.domain)
		if derr != nil {
			return derr
		}
		picName := imgName
		if i := strings.Index(imgName, "."); i > 0 {
			picName = imgName[:i]
		}
		outBytes, outExt := dlDecodeImageBytes(rawBytes, epsID, meta.scrambleID, picName, imgName, isGIF, lossless)
		base := strings.TrimSuffix(filepath.Base(imgName), filepath.Ext(imgName))
		outPath := filepath.Join(chapterFolder, base+outExt)
		if werr := os.WriteFile(outPath, outBytes, 0o644); werr != nil {
			return werr
		}
		if onImage != nil {
			onImage()
		}
	}
	return nil
}

func (m *downloadTaskManager) execute(t *downloadTask) error {
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

	// 新源（如哔咔）图片为直链、无需置乱解码，走独立执行器，复用同一套任务与打包设施。
	if isBikaSource(t.Source) {
		return m.executeBika(t)
	}

	m.update(taskID, func(t *downloadTask) {
		t.Status = "downloading"
		t.Stage = "downloading"
		t.Message = "Downloading..."
		t.DownloadedImages = 0
		t.TotalImages = 0
		t.Percent = 0.0
	})

	ck := store.LoadCookies(t.identity)
	metas, total, err := dlCollectChapterMetas(t.Chapters, ck)
	if err != nil {
		return err
	}
	if total <= 0 {
		return fmt.Errorf("No images found for selected chapters")
	}

	m.update(taskID, func(t *downloadTask) { t.TotalImages = total; t.Message = "Downloading images..." })
	m.recompute(taskID)

	downloaded := 0
	for _, meta := range metas {
		serr := dlSaveChapterImages(rootOut, meta, t.Lossless, func() {
			downloaded++
			d := downloaded
			m.update(taskID, func(t *downloadTask) { t.DownloadedImages = d })
			m.recompute(taskID)
		})
		if serr != nil {
			return serr
		}
	}

	return dlFinalizeZip(m, taskID, t, taskDir, workDir, rootOut)
}

// dlFinalizeZip 是各源共用的收尾：统计文件数 → 打包 → 标记完成。
func dlFinalizeZip(m *downloadTaskManager, taskID string, t *downloadTask, taskDir, workDir, rootOut string) error {
	m.update(taskID, func(t *downloadTask) { t.Stage = "zipping"; t.Message = "Packaging..." })
	m.recompute(taskID)

	zipDir := filepath.Join(taskDir, "zips")
	if err := os.MkdirAll(zipDir, 0o755); err != nil {
		return err
	}
	zipBase := t.AlbumID
	if t.AlbumTitle != "" {
		zipBase = dlSafeName(t.AlbumTitle, 80)
	}
	zipPath := filepath.Join(zipDir, fmt.Sprintf("%s_%s.zip", zipBase, taskID[:8]))

	fileCount := 0
	werr := filepath.Walk(rootOut, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			fileCount++
		}
		return nil
	})
	if werr != nil {
		return werr
	}
	m.update(taskID, func(t *downloadTask) { t.TotalZipFiles = fileCount; t.ZippedFiles = 0 })
	m.recompute(taskID)

	zerr := zipTree(rootOut, zipPath, workDir, func(done, total int) {
		m.update(taskID, func(t *downloadTask) { t.ZippedFiles = done })
		m.recompute(taskID)
	})
	if zerr != nil {
		return zerr
	}

	_ = os.RemoveAll(workDir)

	m.update(taskID, func(t *downloadTask) {
		t.Status = "completed"
		t.Stage = "completed"
		t.Message = "Completed"
		t.ZipPath = zipPath
		t.Percent = 1.0
	})
	return nil
}

func zipTree(srcRoot, zipPath, arcBase string, progress func(done, total int)) error {
	type zfile struct{ path, rel string }
	files := []zfile{}
	werr := filepath.Walk(srcRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(arcBase, p)
		if rerr != nil {
			return rerr
		}
		files = append(files, zfile{p, rel})
		return nil
	})
	if werr != nil {
		return werr
	}
	out, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	for i, f := range files {
		fh := &zip.FileHeader{Name: filepath.ToSlash(f.rel), Method: zip.Deflate}
		fw, ferr := zw.CreateHeader(fh)
		if ferr != nil {
			_ = zw.Close()
			_ = out.Close()
			return ferr
		}
		data, derr := os.ReadFile(f.path)
		if derr != nil {
			_ = zw.Close()
			_ = out.Close()
			return derr
		}
		if _, werr2 := fw.Write(data); werr2 != nil {
			_ = zw.Close()
			_ = out.Close()
			return werr2
		}
		if progress != nil {
			progress(i+1, len(files))
		}
	}
	if cerr := zw.Close(); cerr != nil {
		_ = out.Close()
		return cerr
	}
	return out.Close()
}

var (
	taskManager = newDownloadTaskManager(filepath.Join(store.DataDir(), "downloads", "tasks"))
	dlAlbumsDir = filepath.Join(store.DataDir(), "downloads")
)

func init() {
	go dlAutoCleanupLoop()
}

func dlCleanupCache(keepDays int) (int, int) {
	bases := []string{filepath.Join(store.DataDir(), "downloads", "tasks")}
	now := time.Now()
	if keepDays < 0 {
		keepDays = 0
	}
	removedDirs, removedWork := 0, 0
	for _, base := range bases {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(base, e.Name())
			mtime := now
			if fi, ferr := e.Info(); ferr == nil {
				mtime = fi.ModTime()
			}
			work := filepath.Join(p, "work")
			if fi2, ferr := os.Stat(work); ferr == nil && fi2.IsDir() {
				_ = os.RemoveAll(work)
				removedWork++
			}
			if now.Sub(mtime) <= time.Duration(keepDays)*24*time.Hour {
				continue
			}
			zips := filepath.Join(p, "zips")
			if zs, zerr := os.ReadDir(zips); zerr == nil && len(zs) > 0 {
				continue
			}
			_ = os.RemoveAll(p)
			removedDirs++
		}
	}
	return removedDirs, removedWork
}

// 按占用空间自动清理：每 8 分钟扫描一次；下载目录总大小超过 15GiB 时，
// 从最旧开始删除超过 3 分钟的下载产物，直到总量回落到限制以内（保留窗口保护刚生成的产物）
const (
	dlCleanupScanInterval = 8 * time.Minute
	dlCleanupSizeLimit    = int64(15) << 30 // 15GiB
	dlCleanupMinAge       = 3 * time.Minute // 最近 3 分钟内新增的产物保留
)

// dlDirSize 递归统计目录总字节数
func dlDirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// dlAutoCleanupOnce 总大小未超限时不动；超限时按修改时间从旧到新删除超过保留窗口的产物
// （v2 任务目录、legacy zips、顶层残留），直至回落到限制以内。仍在执行中的任务目录跳过
func dlAutoCleanupOnce() (removed int, freed int64) {
	now := time.Now()
	size := dlDirSize(dlAlbumsDir)
	if size <= dlCleanupSizeLimit {
		return 0, 0
	}

	type cand struct {
		path string
		m    time.Time
	}
	cands := []cand{}

	// v2 任务目录（含 ZIP）
	tasksBase := filepath.Join(dlAlbumsDir, "tasks")
	if entries, err := os.ReadDir(tasksBase); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(tasksBase, e.Name())
			fi, ierr := e.Info()
			if ierr != nil {
				continue
			}
			if now.Sub(fi.ModTime()) < dlCleanupMinAge || taskManager.isActive(e.Name()) {
				continue
			}
			cands = append(cands, cand{path: p, m: fi.ModTime()})
		}
	}

	// legacy zips
	zipsBase := filepath.Join(dlAlbumsDir, "zips")
	if entries, err := os.ReadDir(zipsBase); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(zipsBase, e.Name())
			fi, ierr := e.Info()
			if ierr != nil {
				continue
			}
			if now.Sub(fi.ModTime()) < dlCleanupMinAge {
				continue
			}
			cands = append(cands, cand{path: p, m: fi.ModTime()})
		}
	}

	// 下载目录顶层的其他残留（如 legacy 图片目录）
	if entries, err := os.ReadDir(dlAlbumsDir); err == nil {
		for _, e := range entries {
			name := e.Name()
			if name == "tasks" || name == "zips" {
				continue
			}
			p := filepath.Join(dlAlbumsDir, name)
			fi, ierr := e.Info()
			if ierr != nil {
				continue
			}
			if now.Sub(fi.ModTime()) < dlCleanupMinAge {
				continue
			}
			cands = append(cands, cand{path: p, m: fi.ModTime()})
		}
	}

	// 从最旧开始删，直到总量回落到限制以内
	sort.Slice(cands, func(i, j int) bool { return cands[i].m.Before(cands[j].m) })
	for _, c := range cands {
		if size <= dlCleanupSizeLimit {
			break
		}
		s := dlDirSize(c.path)
		if rerr := os.RemoveAll(c.path); rerr != nil {
			continue
		}
		size -= s
		if s > 0 {
			removed++
			freed += s
		}
	}
	return removed, freed
}

// dlAutoCleanupLoop 后台循环：启动即清理一次，此后每 8 分钟扫描
func dlAutoCleanupLoop() {
	dlAutoCleanupOnce()
	ticker := time.NewTicker(dlCleanupScanInterval)
	defer ticker.Stop()
	for range ticker.C {
		dlAutoCleanupOnce()
	}
}
