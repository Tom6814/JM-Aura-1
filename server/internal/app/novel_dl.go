package app

import (
	"archive/zip"
	"context"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"jmaura/internal/store"
)

// 小说导出：与图片下载 (dl.go) 平行的文本流水线。
// 复用 downloadTaskManager 的阶段/进度语汇（queued/downloading/zipping/completed/failed/cancelled），
// 但不复用其字段，避免污染图片专用计数器。
// 繁简由 JM 服务端原生处理（NovelChapter 的 lang 参数），无需转换库。

type novelExportChapter struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Order int    `json:"order"`
}

type novelExportTask struct {
	TaskID     string
	NovelID    string
	NovelTitle string
	Author     string
	Chapters   []novelExportChapter
	Lang       string // "tw" 繁体 | "cn" 简体
	Format     string // "txt" | "epub"
	Status     string
	Stage      string
	Message    string
	CreatedAt  time.Time
	UpdatedAt  time.Time

	TotalChapters      int
	DownloadedChapters int
	FailedChapters     int
	Percent            float64
	FilePath           string

	identity string
	// owner 是创建导出任务时的站点用户名；下载功能限登录用户，用于校验任务归属。
	owner string
}

func (t *novelExportTask) toPublic() map[string]any {
	downloadURL := ""
	if t.Status == "completed" && t.FilePath != "" {
		downloadURL = "/api/v2/jm/novel/export/tasks/" + t.TaskID + "/download"
	}
	return map[string]any{
		"task_id":             t.TaskID,
		"novel_id":            t.NovelID,
		"novel_title":         t.NovelTitle,
		"author":              t.Author,
		"lang":                t.Lang,
		"format":              t.Format,
		"status":              t.Status,
		"stage":               t.Stage,
		"message":             t.Message,
		"total_chapters":      t.TotalChapters,
		"downloaded_chapters": t.DownloadedChapters,
		"failed_chapters":     t.FailedChapters,
		"percent":             math.Round(t.Percent*10000) / 10000,
		"download_url":        downloadURL,
	}
}

type novelExportManager struct {
	baseDir string
	mu      sync.Mutex
	cond    *sync.Cond
	tasks   map[string]*novelExportTask
	pending []string
	conc    int
	// maxQueued 为单个站点账号允许同时排队的导出任务数（默认 10）。
	maxQueued int
}

var novelExporter = newNovelExportManager(filepath.Join(store.DataDir(), "novel_exports"))

func newNovelExportManager(baseDir string) *novelExportManager {
	_ = os.MkdirAll(baseDir, 0o755)
	m := &novelExportManager{
		baseDir:   baseDir,
		tasks:     make(map[string]*novelExportTask),
		conc:      dlConcurrency(),
		maxQueued: dlMaxQueuedPerOwner(),
	}
	m.cond = sync.NewCond(&m.mu)
	for i := 0; i < m.conc; i++ {
		go m.run()
	}
	go m.cleanupLoop()
	return m
}

// queuedByOwnerLocked 统计 owner 仍在排队（status=queued）的任务数，须在 m.mu 内调用。
func (m *novelExportManager) queuedByOwnerLocked(owner string) int {
	n := 0
	for _, t := range m.tasks {
		if t.owner == owner && t.Status == "queued" {
			n++
		}
	}
	return n
}

func (m *novelExportManager) createTask(t *novelExportTask) (*novelExportTask, error) {
	t.TaskID = newTaskUUID()
	t.Status = "queued"
	t.Stage = "queued"
	t.CreatedAt = time.Now()
	t.UpdatedAt = time.Now()
	m.mu.Lock()
	if m.queuedByOwnerLocked(t.owner) >= m.maxQueued {
		m.mu.Unlock()
		return nil, errDlQueueFull
	}
	m.tasks[t.TaskID] = t
	m.pending = append(m.pending, t.TaskID)
	m.mu.Unlock()
	m.cond.Signal()
	return t, nil
}

// snapshot 取回任务快照；仅当任务归属 owner 时才返回 found=true。
func (m *novelExportManager) snapshot(id, owner string) (pub map[string]any, status, filePath string, found bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok || t.owner != owner {
		return nil, "", "", false
	}
	pub = t.toPublic()
	status = t.Status
	filePath = t.FilePath
	if status == "queued" {
		for i, pid := range m.pending {
			if pid == id {
				pub["queue_position"] = i + 1
				pub["queued_ahead"] = i
				break
			}
		}
	}
	return pub, status, filePath, true
}

func (m *novelExportManager) update(id string, fn func(*novelExportTask)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		fn(t)
		t.UpdatedAt = time.Now()
	}
}

func (m *novelExportManager) computePercent(t *novelExportTask) float64 {
	switch {
	case t.Status == "completed":
		return 1.0
	case t.Status == "failed":
		return t.Percent
	case t.Stage == "zipping":
		if t.TotalChapters > 0 {
			r := float64(t.DownloadedChapters) / float64(t.TotalChapters)
			if r > 1 {
				r = 1
			}
			return 0.9 + 0.1*r
		}
		return 0.9
	case t.Stage == "downloading":
		if t.TotalChapters > 0 {
			r := float64(t.DownloadedChapters) / float64(t.TotalChapters)
			if r > 1 {
				r = 1
			}
			return 0.9 * r
		}
	}
	return 0.0
}

func (m *novelExportManager) recompute(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		t.Percent = m.computePercent(t)
	}
}

func (m *novelExportManager) cancelQueued(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok || t.Status != "queued" {
		return false
	}
	t.Status = "failed"
	t.Stage = "cancelled"
	t.Message = "已取消"
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

func (m *novelExportManager) isActive(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return false
	}
	return t.Status == "queued" || t.Status == "downloading" || t.Status == "zipping"
}

func (m *novelExportManager) run() {
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
					m.update(id, func(t *novelExportTask) {
						t.Status = "failed"
						t.Stage = "failed"
						t.Message = fmt.Sprintf("%v", rec)
					})
				}
				m.recompute(id)
			}()
			if err := m.execute(t); err != nil {
				m.update(id, func(t *novelExportTask) {
					t.Status = "failed"
					t.Stage = "failed"
					t.Message = err.Error()
				})
			}
		}()
	}
}

type novelChapterContent struct {
	Title string
	Text  string
}

func (m *novelExportManager) execute(t *novelExportTask) error {
	if len(t.Chapters) == 0 {
		return fmt.Errorf("未选择章节")
	}
	taskDir := filepath.Join(m.baseDir, t.TaskID)
	workDir := filepath.Join(taskDir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}

	m.update(t.TaskID, func(t *novelExportTask) {
		t.Status = "downloading"
		t.Stage = "downloading"
		t.Message = "正在获取章节…"
		t.TotalChapters = len(t.Chapters)
		t.DownloadedChapters = 0
		t.FailedChapters = 0
		t.Percent = 0.0
	})
	m.recompute(t.TaskID)

	ck := store.LoadCookies(t.identity)
	contents := make([]novelChapterContent, 0, len(t.Chapters))
	failed := 0
	for _, ch := range t.Chapters {
		c, err := novelFetchChapter(t.Lang, ch.ID, ch.Title, ck)
		if err != nil {
			failed++
		} else {
			contents = append(contents, c)
		}
		done := len(contents) + failed
		f := failed
		m.update(t.TaskID, func(t *novelExportTask) {
			t.DownloadedChapters = done
			t.FailedChapters = f
		})
		m.recompute(t.TaskID)
	}
	if len(contents) == 0 {
		return fmt.Errorf("所选章节均获取失败")
	}

	m.update(t.TaskID, func(t *novelExportTask) {
		t.Stage = "zipping"
		t.Message = "正在合成文件…"
	})
	m.recompute(t.TaskID)

	outDir := filepath.Join(taskDir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	base := dlSafeName(t.NovelTitle, 60)
	if base == "" || base == "untitled" {
		base = dlSafeName(t.NovelID, 40)
	}
	outPath := filepath.Join(outDir, fmt.Sprintf("%s_%s.%s", base, novelLangLabel(t.Lang), t.Format))

	switch t.Format {
	case "epub":
		if err := novelBuildEPUB(outPath, t, contents); err != nil {
			return err
		}
	default:
		if err := os.WriteFile(outPath, []byte(novelBuildTXT(t, contents)), 0o644); err != nil {
			return err
		}
	}

	_ = os.RemoveAll(workDir)

	msg := "已完成"
	if failed > 0 {
		msg = fmt.Sprintf("已完成（%d 章获取失败已跳过）", failed)
	}
	m.update(t.TaskID, func(t *novelExportTask) {
		t.Status = "completed"
		t.Stage = "completed"
		t.Message = msg
		t.FilePath = outPath
		t.Percent = 1.0
	})
	return nil
}

func novelFetchChapter(lang, cid, fallbackTitle string, ck map[string]string) (novelChapterContent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	res, err := apiClient().NovelChapter(ctx, cid, lang, ck)
	if err != nil {
		return novelChapterContent{}, err
	}
	d := adaptNovelChapter(decodeRawMap(res.Data))
	title := strings.TrimSpace(pyStr(d["title"]))
	if title == "" {
		title = fallbackTitle
	}
	return novelChapterContent{Title: title, Text: novelHTMLToText(pyStr(d["content"]))}, nil
}

func novelLangLabel(lang string) string {
	if lang == "cn" {
		return "简体"
	}
	return "繁体"
}

var (
	reNovelBr     = regexp.MustCompile(`(?is)<\s*br\s*/?\s*>`)
	reNovelBlockE = regexp.MustCompile(`(?is)</\s*(p|div|h[1-6]|li|tr|section|article|blockquote)\s*>`)
	reNovelBlockS = regexp.MustCompile(`(?is)<\s*(p|div|h[1-6]|li|tr|section|article|blockquote)[^>]*>`)
	reNovelTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	reNovelBlanks = regexp.MustCompile(`\n{3,}`)
)

// novelHTMLToText 将 JM 章节 HTML 规整为纯文本（段落用空行分隔），供 txt 与 epub 共用。
func novelHTMLToText(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = reNovelBr.ReplaceAllString(s, "\n")
	s = reNovelBlockE.ReplaceAllString(s, "\n\n")
	s = reNovelBlockS.ReplaceAllString(s, "\n")
	s = reNovelTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\u00a0", " ")

	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t")
	}
	s = strings.Join(lines, "\n")
	s = reNovelBlanks.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func novelChapterHeading(i int, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Sprintf("第 %d 章", i+1)
	}
	if strings.HasPrefix(title, "第") {
		return title
	}
	return fmt.Sprintf("第 %d 章 %s", i+1, title)
}

func novelBuildTXT(t *novelExportTask, chapters []novelChapterContent) string {
	var b strings.Builder
	b.WriteString(t.NovelTitle)
	b.WriteString("\n")
	if t.Author != "" {
		b.WriteString("作者：" + t.Author + "\n")
	}
	fmt.Fprintf(&b, "共 %d 章 · %s\n", len(chapters), novelLangLabel(t.Lang))
	b.WriteString("\n")
	for i, ch := range chapters {
		b.WriteString("========================================\n\n")
		b.WriteString(novelChapterHeading(i, ch.Title))
		b.WriteString("\n\n")
		b.WriteString(ch.Text)
		b.WriteString("\n\n")
	}
	return b.String()
}

// ---- EPUB（EPUB 3，复用 archive/zip，无第三方依赖）----

const novelEPUBStyle = `body { font-family: serif; line-height: 1.8; margin: 1em; }
h1.chapter-title { font-size: 1.3em; margin: 0.6em 0 1em; }
p { margin: 0 0 0.9em; text-indent: 2em; }
`

func novelContainerXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">` + "\n" +
		`  <rootfiles>` + "\n" +
		`    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>` + "\n" +
		`  </rootfiles>` + "\n" +
		`</container>` + "\n"
}

func novelTextToBody(text string) string {
	var b strings.Builder
	for _, p := range strings.Split(text, "\n\n") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		esc := html.EscapeString(p)
		esc = strings.ReplaceAll(esc, "\n", "<br/>")
		b.WriteString("<p>" + esc + "</p>\n")
	}
	return b.String()
}

func novelChapterXHTML(title, text string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE html>` + "\n")
	b.WriteString(`<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="zh" lang="zh">` + "\n")
	b.WriteString("<head>\n<meta charset=\"utf-8\"/>\n")
	b.WriteString("<title>" + html.EscapeString(title) + "</title>\n")
	b.WriteString("<link rel=\"stylesheet\" type=\"text/css\" href=\"style.css\"/>\n")
	b.WriteString("</head>\n<body>\n")
	b.WriteString("<h1 class=\"chapter-title\">" + html.EscapeString(title) + "</h1>\n")
	b.WriteString(novelTextToBody(text))
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

func novelNavXHTML(chapters []novelChapterContent) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE html>` + "\n")
	b.WriteString(`<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="zh" lang="zh">` + "\n")
	b.WriteString("<head>\n<meta charset=\"utf-8\"/>\n<title>目录</title>\n</head>\n<body>\n")
	b.WriteString("<nav epub:type=\"toc\" id=\"toc\">\n<h1>目录</h1>\n<ol>\n")
	for i, ch := range chapters {
		fmt.Fprintf(&b, "<li><a href=\"chapter_%04d.xhtml\">%s</a></li>\n", i+1, html.EscapeString(novelChapterHeading(i, ch.Title)))
	}
	b.WriteString("</ol>\n</nav>\n</body>\n</html>\n")
	return b.String()
}

func novelOPF(t *novelExportTask, count int) string {
	author := t.Author
	if author == "" {
		author = "佚名"
	}
	title := t.NovelTitle
	if title == "" {
		title = t.NovelID
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id">` + "\n")
	b.WriteString(`  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">` + "\n")
	b.WriteString(`    <dc:identifier id="pub-id">urn:uuid:` + t.TaskID + `</dc:identifier>` + "\n")
	b.WriteString(`    <dc:title>` + html.EscapeString(title) + `</dc:title>` + "\n")
	b.WriteString(`    <dc:language>zh</dc:language>` + "\n")
	b.WriteString(`    <dc:creator>` + html.EscapeString(author) + `</dc:creator>` + "\n")
	b.WriteString(`    <meta property="dcterms:modified">` + time.Now().UTC().Format("2006-01-02T15:04:05Z") + `</meta>` + "\n")
	b.WriteString("  </metadata>\n  <manifest>\n")
	b.WriteString(`    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>` + "\n")
	b.WriteString(`    <item id="css" href="style.css" media-type="text/css"/>` + "\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&b, "    <item id=\"c%04d\" href=\"chapter_%04d.xhtml\" media-type=\"application/xhtml+xml\"/>\n", i+1, i+1)
	}
	b.WriteString("  </manifest>\n  <spine>\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&b, "    <itemref idref=\"c%04d\"/>\n", i+1)
	}
	b.WriteString("  </spine>\n</package>\n")
	return b.String()
}

func novelBuildEPUB(path string, t *novelExportTask, chapters []novelChapterContent) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	abort := func(e error) error {
		_ = zw.Close()
		_ = out.Close()
		return e
	}

	// mimetype 必须是首个条目且不压缩（EPUB 规范）
	mh := &zip.FileHeader{Name: "mimetype", Method: zip.Store}
	mw, err := zw.CreateHeader(mh)
	if err != nil {
		return abort(err)
	}
	if _, err := mw.Write([]byte("application/epub+zip")); err != nil {
		return abort(err)
	}

	add := func(name, content string) error {
		fh := &zip.FileHeader{Name: name, Method: zip.Deflate}
		fw, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		_, err = fw.Write([]byte(content))
		return err
	}

	if err := add("META-INF/container.xml", novelContainerXML()); err != nil {
		return abort(err)
	}
	for i, ch := range chapters {
		if err := add(fmt.Sprintf("OEBPS/chapter_%04d.xhtml", i+1), novelChapterXHTML(novelChapterHeading(i, ch.Title), ch.Text)); err != nil {
			return abort(err)
		}
	}
	if err := add("OEBPS/style.css", novelEPUBStyle); err != nil {
		return abort(err)
	}
	if err := add("OEBPS/nav.xhtml", novelNavXHTML(chapters)); err != nil {
		return abort(err)
	}
	if err := add("OEBPS/content.opf", novelOPF(t, len(chapters))); err != nil {
		return abort(err)
	}

	if err := zw.Close(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// ---- 自清理：文本产物体积很小，仅在任务目录过期后回收，不占用常驻内存 ----

const (
	novelExportRetention = 24 * time.Hour
	novelExportScanEvery = 30 * time.Minute
)

func (m *novelExportManager) cleanupOnce() {
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if m.isActive(name) {
			continue
		}
		fi, ierr := e.Info()
		if ierr != nil {
			continue
		}
		if now.Sub(fi.ModTime()) < novelExportRetention {
			continue
		}
		_ = os.RemoveAll(filepath.Join(m.baseDir, name))
		m.mu.Lock()
		delete(m.tasks, name)
		m.mu.Unlock()
	}
}

func (m *novelExportManager) cleanupLoop() {
	m.cleanupOnce()
	ticker := time.NewTicker(novelExportScanEvery)
	defer ticker.Stop()
	for range ticker.C {
		m.cleanupOnce()
	}
}
