package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"unicode"

	"jmaura/internal/store"
)

// 内容过滤（屏蔽）：按「访客 + 源」保存规则，服务端在返回内容前统一过滤。
//
// 重活全在这里：前端只负责「读写一份规则 + 展示命中标记」，不做任何匹配逻辑。
//
// 设计取向（对齐 Breeze）：只有一个输入框 + 词条列表，不做维度拆分。
// 每个词条都是**全局屏蔽词**：只要 ID / 标题 / 作者 / 标签 / 评论 任一字段命中
// （大小写不敏感的子串），就整体屏蔽。另提供「一键预设」（如 NTR）。
//
// 规则文件 filters.json：
//
//	{ "v": 1, "users": { "<访客>": { "jm": {...规则...}, "bika": {...} } } }
//
// 单份规则：
//
//	{
//	  "words": ["牛头人", "1232836"],
//	  "presets": { "ntr": false }
//	}
//
// 屏蔽始终开启（没有总开关）；详情页命中即遮挡。
//
// 行为约定：
//   - 列表（首页/搜索/随机/每日精选/相关推荐）：命中即**直接从结果剔除**，
//     不给「让我看看」——没点开之前不暴露。
//   - 详情页（已主动点开）：命中即打标记，前端先遮挡，给「让我看看」再展开。
//   - 评论：不作为「删评论」处理；详情页评论区**只要命中**（>=1 条不同评论）就把整个作品
//     遮挡并给警告；当命中达到 maskCommentBlacklistHits 条时，进一步记入 Auto 黑名单
//     持久化，之后列表/详情一并隐藏（评论本身原样返回）。
//
// 访客键 = 站点账号（登录）或 g:<gid>（游客），与其它 store 的隔离键口径一致；
// 因此换设备/清 Cookie 才会丢规则，登录账号之间互不影响。

const (
	maskFile     = "filters.json"
	maskMaxWords = 500  // 最多保存的词条数
	maskMaxRunes = 64   // 单个词条的最大长度（rune）
	maskMaxAuto  = 2000 // 自动屏蔽名单上限（作品数）
	// maskCommentBlacklistHits 是「评论命中 → 拉黑整个作品」的门槛（只管持久化，不管遮挡）：
	// 详情页评论区只要命中（>=1 条不同评论）就先遮挡并给「让我看看」的警告；
	// 当同一作品有这么多条不同评论各命中时，才进一步记入 Auto 黑名单持久化
	// （之后列表/详情直接隐藏）。
	maskCommentBlacklistHits = 3
)

type maskRules struct {
	Words []string `json:"words"`
	// Presets 是「一键预设」的开关（键固定，见 maskPresetList）；开启后自动并入匹配，
	// 无需用户逐个维护词条。语料留在后端，前端只发一个开关布尔值。
	Presets map[string]bool `json:"presets"`
	// Auto 是「评论命中 → 自动屏蔽」的作品 ID 名单（持久化）：详情页扫评论命中后记下，
	// 之后列表/详情都会按它隐藏，无需再拉评论。
	Auto []string `json:"auto,omitempty"`
	// search 是匹配用词表：Words 每个词条再展开其简/繁写法后的并集。仅内存使用，
	// 不落盘也不下发（json:"-"），所以界面上始终只显示用户输入的那一个词。
	search []string `json:"-"`
	// autoSet 是 Auto 的查询用集合（内存，不落盘）。
	autoSet map[string]bool `json:"-"`
}

// maskPreset 是一个内置预设（一组同义关键词 + 是否也匹配标题）。
type maskPreset struct {
	Key        string
	Name       string // 命中原因里显示的短名，如 "NTR"
	Label      string // 设置页开关的标题
	Hint       string // 设置页开关的说明
	Words      []string
	MatchTitle bool
	Search     []string // 展开简繁后的匹配词表（init 时填充）
}

// maskPresetList 内置预设清单（顺序即前端渲染顺序）。
//
// 「屏蔽 NTR」是一个**同类题材合集**的预设：牛头人 / 出轨 / 嫖娼 / 强制 等。语料为
// 常见的中/日/英标签与标题写法；大小写不敏感的子串匹配（"netora" 一并命中
// netorare/netorase/netori）。词表偏激进，宁可多挡；如需收窄告诉我删哪几条即可。
var maskPresetList = []maskPreset{
	{
		Key:        "ntr",
		Name:       "NTR",
		Label:      "屏蔽 NTR（牛头人）",
		Hint:       "牛头人 / 人妻 / 出轨 / 嫖娼 / 强制 / 哥布林 等同类（中·日·英）",
		MatchTitle: true,
		Words: []string{
			// 牛头人 / 寝取（"寝取" 已覆盖 寝取り/寝取られ/寝取らせ；"netora" 覆盖 netorare/netorase）
			"NTR", "有牛", "牛头人", "寝取", "寝盗", "ネトラレ", "ネトリ", "netora", "netori",
			// 人妻 / 已婚（"老婆 / 娇妻" 较泛，若误伤多可删）
			"人妻", "少妇", "未亡人", "娇妻", "老婆", "NTR妻", "寝取妻", "cheating wife", "hitozuma",
			// 出轨 / 背叛 / 夺爱
			"出軌", "出轨", "外遇", "不倫", "浮気", "夫目前犯", "cuckold", "横刀夺爱", "略夺爱", "BSS",
			// 嫖娼 / 性交易
			"嫖娼", "嫖客", "娼妓", "妓女", "援交", "卖淫", "賣淫", "卖春", "買春", "买春",
			// 滥交 / 一女多男取向（只收「女方被多人/滥交」这类词；**不收** 乱交/群交/orgy/淫乱/淫荡
			// 这些方向中立或过宽泛的词，避免误伤「一男多女」后宫题材）
			"滥交", "荡妇", "gangbang", "gang bang", "promiscuous",
			// 强制 / 非自愿 / 胁迫
			"强奸", "強姦", "轮奸", "輪姦", "迷奸", "睡奸", "强制", "强迫", "レイプ", "胁迫", "blackmail",
			// 怪物 / 凌辱系（哥布林等）：故意避开 "orc" 这类易误伤的子串
			"哥布林", "ゴブリン", "goblin", "オーク", "兽人", "獸人", "触手",
			"异种姦", "異種姦", "异种奸", "凌辱", "陵辱", "强暴", "強暴", "肉便器", "苗床",
			// 托卵 / 种付 / 孕（NTR 常见收尾）
			"托卵", "种付け", "孕ませ",
			// 调教 / 洗脑 / 堕落 / 精神崩坏（较泛，如嫌太狠可删）
			"催眠", "洗脑", "洗腦", "調教", "调教", "堕落", "恶堕", "快楽堕", "mindbreak", "精神操作",
		},
	},
}

var maskMu sync.Mutex // 串行化「读-改-写」，避免并发保存互相覆盖

// init 为每个预设预展开简/繁写法，避免每次匹配重复转换。
func init() {
	for i := range maskPresetList {
		var expanded []string
		for _, w := range maskPresetList[i].Words {
			expanded = append(expanded, maskWordVariants(w)...)
		}
		maskPresetList[i].Search = maskDedupe(expanded)
	}
}

// —— 存储 ——

func maskLoadDoc() map[string]any {
	if doc, ok := store.LoadJSON(maskFile); ok {
		if _, ok := doc["users"].(map[string]any); !ok {
			doc["users"] = map[string]any{}
		}
		doc["v"] = 1
		return doc
	}
	return map[string]any{"v": 1, "users": map[string]any{}}
}

func maskVisitor(r *http.Request) string {
	if u, _ := siteUserOf(r); strings.TrimSpace(u) != "" {
		return strings.TrimSpace(u)
	}
	return "anon"
}

// maskRulesFor 读取某访客在某源下的规则（缺失时返回默认规则）。
func maskRulesFor(visitor, source string) maskRules {
	doc := maskLoadDoc()
	users, _ := doc["users"].(map[string]any)
	bucket, _ := users[visitor].(map[string]any)
	raw, _ := bucket[source].(map[string]any)
	out := maskRules{}
	if len(raw) > 0 {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, &out)
		}
	}
	out.prep()
	return out
}

// maskSave 写入某访客在某源下的规则（已规范化）。
func maskSave(visitor, source string, rules maskRules) {
	rules.prep()
	maskMu.Lock()
	defer maskMu.Unlock()
	doc := maskLoadDoc()
	users, _ := doc["users"].(map[string]any)
	bucket, _ := users[visitor].(map[string]any)
	if bucket == nil {
		bucket = map[string]any{}
		users[visitor] = bucket
	}
	bucket[source] = rules
	doc["v"] = 1
	_ = store.SaveJSON(maskFile, doc)
}

// —— 规则规范化与匹配 ——

// prep 清洗词条（去空白、按大小写不敏感去重、限长限量）并规范化预设开关。
func (f *maskRules) prep() {
	f.Words = maskCleanWords(f.Words)
	// 匹配用词表：把每个词条展开成简/繁写法（界面仍只显示用户输入的那一个词）。
	var expanded []string
	for _, w := range f.Words {
		expanded = append(expanded, maskWordVariants(w)...)
	}
	f.search = maskDedupe(expanded)
	// 自动屏蔽名单：去重、限量（保留最近的），并构建查询集合。
	auto := maskDedupe(f.Auto)
	if len(auto) > maskMaxAuto {
		auto = auto[len(auto)-maskMaxAuto:]
	}
	f.Auto = auto
	f.autoSet = make(map[string]bool, len(auto))
	for _, id := range auto {
		f.autoSet[strings.ToLower(id)] = true
	}
	// 只保留已知预设键，值缺失按 false；保证序列化形状稳定（前端好渲染）。
	next := make(map[string]bool, len(maskPresetList))
	for _, p := range maskPresetList {
		next[p.Key] = f.Presets[p.Key]
	}
	f.Presets = next
}

// maskDedupe 去空白、按大小写不敏感去重、限长（不做数量上限，供展开后的词表使用）。
func maskDedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, w := range in {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if r := []rune(w); len(r) > maskMaxRunes {
			w = string(r[:maskMaxRunes])
		}
		key := strings.ToLower(w)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, w)
	}
	return out
}

func maskCleanWords(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, w := range in {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if r := []rune(w); len(r) > maskMaxRunes {
			w = string(r[:maskMaxRunes])
		}
		key := strings.ToLower(w)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, w)
		if len(out) >= maskMaxWords {
			break
		}
	}
	return out
}

// active 报告这份规则是否可能命中任何内容。（屏蔽常开，无总开关。）
func (f maskRules) active() bool {
	if len(f.search) > 0 || len(f.autoSet) > 0 {
		return true
	}
	for _, p := range maskPresetList {
		if f.Presets[p.Key] && len(p.Search) > 0 {
			return true
		}
	}
	return false
}

func maskHitAny(words []string, fields ...string) string {
	for _, w := range words {
		lw := strings.ToLower(w)
		for _, f := range fields {
			if strings.Contains(strings.ToLower(f), lw) {
				return w
			}
		}
	}
	return ""
}

func maskDeref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// maskCompact 去掉空白与常见分隔符并转小写，用于「作者写法不一致」的容错比较。
// 例如详情页作者是 "Kim, Jong, Geon"，而列表里是 "Kim Jong Geon"——归一化后都是
// "kimjonggeon"，这样从详情页屏蔽作者后，列表里同一作者的作品也能被命中。
func maskCompact(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\u3000', ',', '，', '、', '·', '・', '／', '/':
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// maskHitAuthor 在作者字段上匹配：先按常规大小写不敏感子串，再退化到「忽略分隔符」的
// 归一化比较，专门解决上一条注释里说的详情/列表作者写法不一致问题。
func maskHitAuthor(words []string, author string) string {
	author = strings.TrimSpace(author)
	if author == "" {
		return ""
	}
	if w := maskHitAny(words, author); w != "" {
		return w
	}
	na := maskCompact(author)
	if na == "" {
		return ""
	}
	for _, w := range words {
		if c := maskCompact(w); c != "" && strings.Contains(na, c) {
			return w
		}
	}
	return ""
}

// reason 返回命中原因（未命中返回空串）。词条用大小写不敏感的子串，跨 ID/标题/作者/标签。
func (f maskRules) reason(it v2ComicSummary) string {
	if id := strings.ToLower(strings.TrimSpace(it.ComicID)); id != "" && f.autoSet[id] {
		return "评论命中"
	}
	fields := []string{it.ComicID, it.Title}
	fields = append(fields, it.Tags...)
	if w := maskHitAny(f.search, fields...); w != "" {
		return "屏蔽词：" + w
	}
	// 作者单独比较：容忍 ", " 与空格等分隔符差异（见 maskHitAuthor）。
	if w := maskHitAuthor(f.search, maskDeref(it.Author)); w != "" {
		return "屏蔽词：" + w
	}
	for _, p := range maskPresetList {
		if !f.Presets[p.Key] || len(p.Search) == 0 {
			continue
		}
		if w := maskHitAny(p.Search, it.Tags...); w != "" {
			return "预设 " + p.Name + "：" + w
		}
		if p.MatchTitle {
			if w := maskHitAny(p.Search, it.Title); w != "" {
				return "预设 " + p.Name + "：" + w
			}
		}
	}
	return ""
}

// maskHitComment 报告**单条**评论内容是否命中：先看用户自填词条，再看已启用的预设
// （这样「屏蔽 NTR」预设也会命中评论里出现的相关词）。返回首个命中的词，未命中为空串。
func (f maskRules) maskHitComment(content string) string {
	if w := maskHitAny(f.search, content); w != "" {
		return w
	}
	for _, p := range maskPresetList {
		if !f.Presets[p.Key] || len(p.Search) == 0 {
			continue
		}
		if w := maskHitAny(p.Search, content); w != "" {
			return w
		}
	}
	return ""
}

// maskCommentHits 逐条统计评论区命中的评论条数，返回首个命中的词与命中条数。
// 同一作品里 N 条不同评论各命中即计 N 次（同一条评论出现多次只计 1 次）。
func (f maskRules) maskCommentHits(texts []string) (string, int) {
	first := ""
	hits := 0
	for _, t := range texts {
		if w := f.maskHitComment(t); w != "" {
			hits++
			if first == "" {
				first = w
			}
		}
	}
	return first, hits
}

// —— 应用到响应 ——

// v2MaskList 对发现类列表统一过滤：命中即从数组里剔除（列表不暴露、不给「让我看看」）。
func v2MaskList(r *http.Request, out any) any {
	items, ok := out.([]v2ComicSummary)
	if !ok {
		return out
	}
	rules := maskRulesFor(maskVisitor(r), r.PathValue("source"))
	if !rules.active() {
		return items
	}
	kept := items[:0]
	for _, it := range items {
		if rules.reason(it) != "" {
			continue
		}
		kept = append(kept, it)
	}
	return kept
}

// v2MaskDetail 给「已点开」的详情页打命中标记：命中即遮挡，前端给「让我看看」再展开。
// 除了 ID/标题/作者/标签，还会扫描评论区——评论里出现屏蔽词即视为**整个作品**命中。
func v2MaskDetail(r *http.Request, out any) any {
	detail, ok := out.(v2ComicDetail)
	if !ok {
		return out
	}
	visitor, source := maskVisitor(r), r.PathValue("source")
	rules := maskRulesFor(visitor, source)
	if !rules.active() {
		return detail
	}
	if reason := rules.reason(v2ComicSummary{
		ComicID: detail.ComicID,
		Title:   detail.Title,
		Author:  detail.Author,
		Tags:    detail.Tags,
	}); reason != "" {
		detail.Masked = true
		detail.MaskedReason = reason
		return detail
	}
	// 标题/作者/标签没命中：再看评论区。评论区**只要命中**（>=1 条不同评论）就先遮挡整个
	// 作品并给出警告；若同时达到 maskCommentBlacklistHits 条，才进一步记入 Auto 黑名单
	// 持久化（之后列表/详情都会直接隐藏，无需再拉评论）。
	if word, hits := rules.maskCommentHits(commentTextsForComic(r.Context(), source, effIdentityForRequest(r), detail.ComicID)); hits > 0 {
		detail.Masked = true
		detail.MaskedReason = fmt.Sprintf("评论命中 %d 条：%s", hits, word)
		if hits >= maskCommentBlacklistHits {
			detail.MaskedReason += "（已加入黑名单）"
			if id := strings.TrimSpace(detail.ComicID); id != "" {
				rules.Auto = append(rules.Auto, id)
				maskSave(visitor, source, rules)
			}
		}
	}
	return detail
}

// 评论只作为「作品级判定」的信号：命中即遮挡整个作品并给警告，**不删除任何评论**；
// 命中达到门槛条数才进一步持久化进黑名单。汇总评论文本的逻辑见 v2.go 的 commentTextsForComic。

// —— 兼容 legacy 端点 ——
//
// /api/latest、/api/promote 直接透传 JM 原始 JSON，且 promote 用的是「与访客无关」
// 的共享缓存。因此过滤必须在「取出缓存之后」按访客规则做，绝不能把过滤结果写回缓存。

// reasonRaw 用原始 JM 条目（id/name/author）判定是否命中；原始列表没有标签。
func (f maskRules) reasonRaw(m map[string]any) string {
	id := pyStr(m["id"])
	if id == "" {
		id = pyOrStr(m["album_id"])
	}
	title := pyOrStr(m["name"])
	if title == "" {
		title = pyOrStr(m["title"])
	}
	return f.reason(v2ComicSummary{
		ComicID: id,
		Title:   title,
		Author:  v2StrPtr(authorText(m["author"])),
	})
}

func maskRawComicList(rules maskRules, list []any) []any {
	kept := make([]any, 0, len(list))
	for _, e := range list {
		if m := rawMap(e); m != nil && rules.reasonRaw(m) != "" {
			continue
		}
		kept = append(kept, e)
	}
	return kept
}

// v2MaskLegacy 给 legacy 端点应用过滤：promote=true 时按「分区 → content」处理，
// 否则按「扁平列表 / 带 content 的信封」处理。
func v2MaskLegacy(r *http.Request, data any, promote bool) any {
	rules := maskRulesFor(maskVisitor(r), "jm")
	if !rules.active() {
		return data
	}
	if promote {
		sections, ok := data.([]any)
		if !ok {
			return data
		}
		for _, s := range sections {
			sm := rawMap(s)
			if sm == nil {
				continue
			}
			if content, ok2 := sm["content"].([]any); ok2 {
				sm["content"] = maskRawComicList(rules, content)
			}
		}
		return sections
	}
	if list, ok := data.([]any); ok {
		return maskRawComicList(rules, list)
	}
	if m := rawMap(data); m != nil {
		if content, ok := m["content"].([]any); ok {
			m["content"] = maskRawComicList(rules, content)
		}
	}
	return data
}

// —— HTTP 接口 ——

// maskPresetMeta 下发给前端渲染预设开关的元信息（语料本身不下发，保持前端轻）。
type maskPresetMeta struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	Count int    `json:"count"`
}

// maskFilterView 是 GET/PUT/block 的统一响应：规则本体 + 预设元信息。
type maskFilterView struct {
	maskRules
	PresetsMeta []maskPresetMeta `json:"presets_meta"`
}

func maskView(rules maskRules) maskFilterView {
	metas := make([]maskPresetMeta, 0, len(maskPresetList))
	for _, p := range maskPresetList {
		metas = append(metas, maskPresetMeta{Key: p.Key, Label: p.Label, Hint: p.Hint, Count: len(p.Words)})
	}
	return maskFilterView{maskRules: rules, PresetsMeta: metas}
}

func handleV2FilterGet(w http.ResponseWriter, r *http.Request) {
	v2OK(w, maskView(maskRulesFor(maskVisitor(r), r.PathValue("source"))))
}

func handleV2FilterPut(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var in maskRules
	if err := json.Unmarshal(body, &in); err != nil {
		v2Fail(w, v2ProviderErr("规则格式不正确", 400))
		return
	}
	visitor, source := maskVisitor(r), r.PathValue("source")
	// auto 由服务端维护（评论命中自动记录）。请求没显式带 "auto" 时保留既有名单，
	// 否则设置页每次保存词条/预设都会把它清空。
	if !jsonHasKey(body, "auto") {
		in.Auto = maskRulesFor(visitor, source).Auto
	}
	in.prep()
	maskSave(visitor, source, in)
	v2OK(w, maskView(maskRulesFor(visitor, source)))
}

func jsonHasKey(body []byte, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

type v2MaskBlockRequest struct {
	Word string `json:"word"`
}

// handleV2FilterBlock 一键屏蔽：把一个词追加进全局屏蔽词（前端只需要发一个词）。
// 详情页「屏蔽该漫画」发作品 ID；点标签/作者发对应文本。结果落盘，刷新即生效。
func handleV2FilterBlock(w http.ResponseWriter, r *http.Request) {
	var req v2MaskBlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		v2Fail(w, v2ProviderErr("参数格式不正确", 400))
		return
	}
	word := strings.TrimSpace(req.Word)
	if word == "" {
		v2Fail(w, v2ProviderErr("屏蔽内容不能为空", 400))
		return
	}
	visitor, source := maskVisitor(r), r.PathValue("source")
	rules := maskRulesFor(visitor, source)
	rules.Words = maskCleanWords(append(rules.Words, word))
	maskSave(visitor, source, rules)
	v2OK(w, maskView(maskRulesFor(visitor, source)))
}
