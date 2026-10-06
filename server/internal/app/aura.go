package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"jmaura/internal/store"
)

type auraHistoryPushRequest struct {
	AlbumID    string   `json:"album_id"`
	AlbumTitle *string  `json:"album_title"`
	PhotoID    *string  `json:"photo_id"`
	Title      *string  `json:"title"`
	PageIndex  *int64   `json:"page_index"`
	Timestamp  *int64   `json:"timestamp"`
	Type       *string  `json:"type"`
	ScrollPct  *float64 `json:"scroll_pct"`
	Source     *string  `json:"source"`
	Lang       *string  `json:"lang"`
}

func auraUserOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	u := getSiteUser(r)
	if u == "" {
		writeJSON(w, 401, errSt(StatusNotLogin, "Not authenticated"))
		return "", false
	}
	return u, true
}

func handleAuraHistoryGet(w http.ResponseWriter, r *http.Request) {
	u, okc := auraUserOf(w, r)
	if !okc {
		return
	}
	limit := 50
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil {
			limit = n
		} else {
			httpDetail(w, 422, "Invalid limit")
			return
		}
	}
	writeJSON(w, 200, ok(store.AuraListHistory(u, limit), ""))
}

func handleAuraHistoryPost(w http.ResponseWriter, r *http.Request) {
	u, okc := auraUserOf(w, r)
	if !okc {
		return
	}
	var req auraHistoryPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 200, errSt(StatusUserError, "Invalid request"))
		return
	}
	pageIndex := int64(0)
	hasPage := req.PageIndex != nil
	if hasPage {
		pageIndex = *req.PageIndex
	}
	ts := int64(0)
	if req.Timestamp != nil {
		ts = *req.Timestamp
	}
	itemType := ""
	if req.Type != nil {
		itemType = strings.TrimSpace(*req.Type)
	}
	scrollPct := 0.0
	hasScroll := req.ScrollPct != nil
	if hasScroll {
		scrollPct = *req.ScrollPct
	}
	albumTitle := ""
	if req.AlbumTitle != nil {
		albumTitle = *req.AlbumTitle
	}
	photoID := ""
	if req.PhotoID != nil {
		photoID = *req.PhotoID
	}
	title := ""
	if req.Title != nil {
		title = *req.Title
	}
	source := ""
	if req.Source != nil {
		source = strings.TrimSpace(*req.Source)
	}
	lang := ""
	if req.Lang != nil {
		lang = strings.TrimSpace(*req.Lang)
	}
	aerr := store.AuraPushHistory(u, req.AlbumID, albumTitle, photoID, title, pageIndex, hasPage, ts, itemType, scrollPct, hasScroll, source, lang)
	if aerr != nil {
		msg := aerr.Error()
		if msg == "" {
			msg = "Invalid request"
		}
		writeJSON(w, 200, errSt(StatusUserError, msg))
		return
	}
	writeJSON(w, 200, ok(map[string]any{"status": "success"}, ""))
}
