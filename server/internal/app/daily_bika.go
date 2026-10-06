package app

import (
	"context"
	"encoding/json"
	"math/rand"
	"sync"
	"time"

	"jmaura/internal/store"
)

// 哔咔「每日精选」首页。
//
// 服务器每天随机抽取一次组合并缓存一整天（内存 + 磁盘），当天所有用户看到同一份
// 内容，次日自动刷新——看起来就像一个每天更新的首页。生成每天只发生一次，
// 前端只发一次 GET，不承担任何挑选或轮询逻辑。
//
// 「有选择性的随机」= 在「排序 × 分类 × 页码」这个受控空间里随机取若干页，
// 再叠加若干次上游纯随机单抽；随机种子取自当天日期，因此同一天内即使进程
// 重启，得到的内容也完全一致。

const (
	bikaDailyTarget   = 40 // 首页目标条数
	bikaDailyListDraw = 2  // 随机（排序 × 分类 × 页码）列表抽取次数
	bikaDailyRandoms  = 4  // 上游纯随机单抽次数
	bikaDailyFile     = "bika_daily.json"
)

var (
	bikaDailyMu      sync.Mutex // 保护缓存字段
	bikaDailyDate    string
	bikaDailyItems   []v2ComicSummary
	bikaDailyBuildMu sync.Mutex // 串行化生成，避免并发重复打上游
)

// Daily 实现 provider.Provider：返回当天的精选列表。
func (bikaProvider) Daily(ctx context.Context, identity string) (any, error) {
	return bikaDailyGet(ctx, identity)
}

func bikaDailyToday() string { return time.Now().Format("2006-01-02") }

// bikaDailyGet 返回当天精选：命中内存/磁盘缓存直接返回，否则生成一次并缓存。
func bikaDailyGet(ctx context.Context, identity string) (any, error) {
	date := bikaDailyToday()

	bikaDailyMu.Lock()
	if bikaDailyDate == date && len(bikaDailyItems) > 0 {
		items := bikaDailyItems
		bikaDailyMu.Unlock()
		return items, nil
	}
	bikaDailyMu.Unlock()

	// 生成是「每天一次」的重活：串行化 + 取锁后二次确认，避免并发重复生成。
	bikaDailyBuildMu.Lock()
	defer bikaDailyBuildMu.Unlock()

	bikaDailyMu.Lock()
	if bikaDailyDate == date && len(bikaDailyItems) > 0 {
		items := bikaDailyItems
		bikaDailyMu.Unlock()
		return items, nil
	}
	bikaDailyMu.Unlock()

	if items, ok := bikaDailyLoad(date); ok {
		bikaDailyMu.Lock()
		bikaDailyDate, bikaDailyItems = date, items
		bikaDailyMu.Unlock()
		return items, nil
	}

	items, err := bikaDailyBuild(ctx, identity, date)
	if err != nil {
		return nil, err
	}
	bikaDailyMu.Lock()
	bikaDailyDate, bikaDailyItems = date, items
	bikaDailyMu.Unlock()
	bikaDailySave(date, items)
	return items, nil
}

// bikaDailyBuild 用当天的日期种子抽取并合并去重。
// 走 bikaDrawPoolAuth（preferPublic=true）：优先公共账号，且公共账号 token 失效时
// 会自动重登重试，不会再出现「掉登录却不重新登录」。
func bikaDailyBuild(ctx context.Context, identity, date string) ([]v2ComicSummary, error) {
	seed := bikaDailySeed(date)
	pool, err := bikaDrawPoolAuth(ctx, identity, true, bikaDailyListDraw, bikaDailyRandoms, seed)
	if err != nil {
		return nil, err
	}
	return v2PickUnique(rand.New(rand.NewSource(seed)), pool, bikaDailyTarget), nil
}

// bikaDailySeed 由日期派生随机种子（FNV-1a），保证「同一天同一份内容」。
func bikaDailySeed(date string) int64 {
	h := int64(1469598103934665603)
	for i := 0; i < len(date); i++ {
		h ^= int64(date[i])
		h *= 1099511628211
	}
	return h
}

// —— 磁盘缓存：重启后当天内容不重排 ——
//
// 以结构体读回，保证内存路径与磁盘路径的输出字节完全一致
// （[]any + map 往返会让 JSON 键序变成字典序）。

func bikaDailyLoad(date string) ([]v2ComicSummary, bool) {
	doc, ok := store.LoadJSON(bikaDailyFile)
	if !ok {
		return nil, false
	}
	if d, _ := doc["date"].(string); d != date {
		return nil, false
	}
	raw, err := json.Marshal(doc["items"])
	if err != nil {
		return nil, false
	}
	var items []v2ComicSummary
	if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
		return nil, false
	}
	return items, true
}

func bikaDailySave(date string, items []v2ComicSummary) {
	_ = store.SaveJSON(bikaDailyFile, map[string]any{"date": date, "items": items})
}
