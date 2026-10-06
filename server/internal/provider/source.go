// Package provider 定义多源聚合的统一扩展点：源标识、能力集、注册表与错误。
//
// 设计目标：让 app 层的 v2 handler 通过 provider.For(source) 拿到对应源的实现，
// 从而在不改动业务逻辑的前提下接入新的内容源（例如 bika）。
package provider

import "fmt"

// Source 是内容源标识，与 URL /api/v2/{source}/... 中的源段一一对应。
type Source string

const (
	// SourceJM 对应原有的 jm 源实现。
	SourceJM Source = "jm"
	// SourceBika 对应哔咔漫画源。
	SourceBika Source = "bika"
)

// Caps 描述一个源支持哪些能力，供前端按源裁剪入口（而不是在每个页面里硬编码判断源）。
type Caps struct {
	Search         bool `json:"search"`
	Categories     bool `json:"categories"`
	Leaderboard    bool `json:"leaderboard"`
	Random         bool `json:"random"`
	ComicDetail    bool `json:"comic_detail"`
	Chapter        bool `json:"chapter"`
	AlsoViewed     bool `json:"also_viewed"`
	Favorite       bool `json:"favorite"`
	FavoriteFolder bool `json:"favorite_folder"`
	Like           bool `json:"like"`
	Comment        bool `json:"comment"`
	CommentLike    bool `json:"comment_like"`
	Novel          bool `json:"novel"`
	NovelExport    bool `json:"novel_export"`
	Download       bool `json:"download"`
	Checkin        bool `json:"checkin"`
	Register       bool `json:"register"`
	Daily          bool `json:"daily"`
}

// UnsupportedError 表示请求的源不存在（未注册）。
type UnsupportedError struct {
	Source string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("Not supported source: %s", e.Source)
}

var registry = map[Source]Provider{}

// Register 注册一个源实现；同一源重复注册会覆盖。
func Register(p Provider) {
	registry[p.Source()] = p
}

// For 按源标识取实现。未注册的源返回 *UnsupportedError。
func For(source string) (Provider, error) {
	p, ok := registry[Source(source)]
	if !ok {
		return nil, &UnsupportedError{Source: source}
	}
	return p, nil
}

// Has 报告某源是否已注册。
func Has(source string) bool {
	_, ok := registry[Source(source)]
	return ok
}
