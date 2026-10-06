package store

import (
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func auraLibFile() string {
	if v := os.Getenv("JM_AURA_AURA_LIBRARY_PATH"); v != "" {
		return v
	}
	return "aura_library.json"
}

func loadAuraDoc() map[string]any {
	if doc, ok := LoadJSON(auraLibFile()); ok {
		if _, ok := doc["users"].(map[string]any); !ok {
			doc["users"] = map[string]any{}
		}
		return doc
	}
	return map[string]any{"v": 1, "users": map[string]any{}}
}

func saveAuraDoc(doc map[string]any) {
	doc["v"] = 1
	_ = SaveJSON(auraLibFile(), doc)
}

func auraBucket(doc map[string]any, user string) map[string]any {
	users, _ := doc["users"].(map[string]any)
	if users == nil {
		users = map[string]any{}
		doc["users"] = users
	}
	u := strings.TrimSpace(user)
	if u == "" {
		panic(errors.New("Missing user"))
	}
	b, _ := users[u].(map[string]any)
	if b == nil {
		b = map[string]any{}
		users[u] = b
	}
	for _, k := range []string{"history"} {
		if _, ok := b[k].(map[string]any); !ok {
			b[k] = map[string]any{}
		}
	}
	return b
}

func nowMS() int64 { return time.Now().UnixMilli() }

func AuraPushHistory(user, albumID, albumTitle, photoID, title string, pageIndex int64, hasPage bool, tsMS int64, itemType string, scrollPct float64, hasScroll bool, source, lang string) error {
	aid := strings.TrimSpace(albumID)
	if aid == "" {
		return errors.New("Missing album_id")
	}
	doc := loadAuraDoc()
	b := auraBucket(doc, user)
	h, _ := b["history"].(map[string]any)
	now := tsMS
	if now <= 0 {
		now = nowMS()
	}
	rec, _ := h[aid].(map[string]any)
	if rec == nil {
		rec = map[string]any{}
		h[aid] = rec
	}
	if albumTitle != "" {
		rec["album_title"] = albumTitle
	}
	if photoID != "" {
		rec["photo_id"] = photoID
	}
	if title != "" {
		rec["title"] = title
	}
	if hasPage {
		if pageIndex < 0 {
			pageIndex = 0
		}
		rec["page_index"] = pageIndex
	}
	if itemType != "" {
		rec["type"] = itemType
	}
	if strings.TrimSpace(source) != "" {
		rec["source"] = strings.TrimSpace(source)
	}
	if strings.TrimSpace(lang) != "" {
		rec["lang"] = strings.TrimSpace(lang)
	}
	if hasScroll {
		if scrollPct < 0 {
			scrollPct = 0
		} else if scrollPct > 1 {
			scrollPct = 1
		}
		rec["scroll_pct"] = scrollPct
	}
	rec["timestamp"] = now
	saveAuraDoc(doc)
	return nil
}

// AuraListHistory returns entries sorted by timestamp desc, limit>=1.
func AuraListHistory(user string, limit int) []map[string]any {
	if limit < 1 {
		limit = 50
	}
	doc := loadAuraDoc()
	b := auraBucket(doc, user)
	h, _ := b["history"].(map[string]any)
	type entry struct {
		key string
		m   map[string]any
	}
	var list []entry
	for aid, v := range h {
		rec, ok := v.(map[string]any)
		if !ok {
			continue
		}
		list = append(list, entry{aid, rec})
	}
	sort.Slice(list, func(i, j int) bool {
		return numF(list[i].m["timestamp"]) > numF(list[j].m["timestamp"])
	})
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		pi := int64(numF(e.m["page_index"]))
		if pi < 0 {
			pi = 0
		}
		itemType := getStr(e.m, "type")
		if itemType == "" {
			itemType = "comic"
		}
		src := strings.TrimSpace(getStr(e.m, "source"))
		if src == "" {
			src = "jm"
		}
		sp := numF(e.m["scroll_pct"])
		if sp < 0 {
			sp = 0
		} else if sp > 1 {
			sp = 1
		}
		out = append(out, map[string]any{
			"album_id":    e.key,
			"album_title": getStr(e.m, "album_title"),
			"photo_id":    getStr(e.m, "photo_id"),
			"title":       getStr(e.m, "title"),
			"page_index":  pi,
			"timestamp":   int64(numF(e.m["timestamp"])),
			"type":        itemType,
			"scroll_pct":  sp,
			"source":      src,
			"lang":        getStr(e.m, "lang"),
		})
	}
	return out
}

func numF(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}
