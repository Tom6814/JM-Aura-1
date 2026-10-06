package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"

	"jmaura/internal/jm"
	"jmaura/internal/store"
)

type configRequest struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	SavePassword *bool  `json:"save_password"`
	AutoLogin    *bool  `json:"auto_login"`
}

type reloginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// applyLoginProfile mirrors the shared post-login block: persist profile and
// extract uid (top-level then nested user/userinfo/profile/member).
func applyLoginProfile(r *http.Request, raw map[string]any) {
	if raw == nil {
		return
	}
	identity := effIdentityOf(r)
	store.JmSetProfile(identity, raw)
	uid := ""
	for _, k := range []string{"uid", "user_id", "id"} {
		if v := raw[k]; v != nil && pyTruthy(v) {
			uid = pyStr(v)
			break
		}
	}
	if uid == "" {
		for _, k := range []string{"user", "userinfo", "profile", "member"} {
			sub, isMap := raw[k].(map[string]any)
			if !isMap {
				continue
			}
			for _, kk := range []string{"uid", "user_id", "id"} {
				if vv := sub[kk]; vv != nil && pyTruthy(vv) {
					uid = pyStr(vv)
					break
				}
			}
			if uid != "" {
				break
			}
		}
	}
	if uid != "" {
		store.JmSetUserID(identity, uid)
	}
}

func handleConfigPost(w http.ResponseWriter, r *http.Request) {
	siteU := getSiteUser(r)
	if siteU == "" {
		httpDetail(w, 401, "Aura login required")
		return
	}
	var req configRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpDetail(w, 422, err.Error())
		return
	}
	_, captured, raw, lerr := jm.NewClient().Login(r.Context(), req.Username, req.Password)
	if lerr != nil {
		httpDetail(w, 401, "Login failed. Please check your username and password.")
		return
	}
	_ = store.SaveCookies(effIdentityOf(r), captured)

	wantSave := (req.SavePassword != nil && *req.SavePassword) || (req.AutoLogin != nil && *req.AutoLogin)
	if wantSave {
		_ = store.CredSet(siteU, req.Username, req.Password)
	}

	applyLoginProfile(r, raw)

	writeJSON(w, 200, map[string]any{
		"status":  "success",
		"message": "Login successful and configuration updated",
		"st":      StatusOK,
		"msg":     "",
	})
}

func handleCredentialsGet(w http.ResponseWriter, r *http.Request) {
	siteU := getSiteUser(r)
	if siteU == "" {
		writeJSON(w, 200, map[string]any{"has_saved": false, "username": "", "st": StatusOK, "msg": ""})
		return
	}
	u := store.CredActiveUsername(siteU)
	writeJSON(w, 200, map[string]any{
		"has_saved": store.CredHas(siteU),
		"username":  u,
		"st":        StatusOK,
		"msg":       "",
	})
}

func handleCredentialsDelete(w http.ResponseWriter, r *http.Request) {
	siteU := getSiteUser(r)
	if siteU == "" {
		writeJSON(w, 200, map[string]any{"status": "success", "st": StatusOK, "msg": ""})
		return
	}
	_ = store.CredClear(siteU)
	writeJSON(w, 200, map[string]any{"status": "success", "st": StatusOK, "msg": ""})
}

func handleSessionRelogin(w http.ResponseWriter, r *http.Request) {
	siteU0 := getSiteUser(r)
	if siteU0 == "" {
		writeJSON(w, 401, errSt(StatusNotLogin, "Aura login required"))
		return
	}
	var req reloginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req = reloginRequest{}
	}
	u := strings.TrimSpace(req.Username)
	p := strings.TrimSpace(req.Password)

	if u == "" || p == "" {
		targetU := u
		if targetU == "" {
			targetU = store.CredActiveUsername(siteU0)
		}
		if targetU != "" {
			_, savedP := store.CredGet(siteU0, "")
			if savedP != "" {
				u = targetU
				p = savedP
			}
		}
	}

	if u == "" || p == "" {
		writeJSON(w, 200, errSt(StatusUserError, "Missing username or password"))
		return
	}

	_, captured, raw, lerr := jm.NewClient().Login(r.Context(), u, p)
	if lerr != nil {
		writeJSON(w, 200, errSt(StatusNotLogin, "Relogin failed"))
		return
	}
	_ = store.SaveCookies(effIdentityOf(r), captured)
	applyLoginProfile(r, raw)
	writeJSON(w, 200, ok(map[string]any{"status": "success"}, ""))
}

func getSavedJMCredentials(r *http.Request) (string, string) {
	rawU, _ := siteUserOf(r)
	activeU := store.CredActiveUsername(rawU)
	if activeU == "" {
		return "", ""
	}
	savedU, savedP := store.CredGet(rawU, activeU)
	return strings.TrimSpace(pyOrStr(savedU, activeU)), strings.TrimSpace(savedP)
}

func reloginFromSavedConfig(r *http.Request) bool {
	u, p := getSavedJMCredentials(r)
	if u == "" || p == "" {
		return false
	}
	_, captured, raw, err := jm.NewClient().Login(r.Context(), u, p)
	if err != nil {
		return false
	}
	_ = store.SaveCookies(effIdentityOf(r), captured)
	applyLoginProfile(r, raw)
	return true
}

// announcementText 读取首访公告内容。兼容两类换行：真实换行，以及字面量 "\n"
// （docker run -e "A=a\nb" 等场景只会传字面量）。空值表示不弹公告。
// 环境变量在进程生命周期内不变，故只解析一次以省去每次请求的字符串处理。
var (
	announcementOnce sync.Once
	announcementVal  string
)

func announcementText() string {
	announcementOnce.Do(func() {
		raw := os.Getenv("JM_AURA_ANNOUNCEMENT")
		if strings.TrimSpace(raw) == "" {
			return
		}
		raw = strings.ReplaceAll(raw, "\r\n", "\n")
		raw = strings.ReplaceAll(raw, "\\n", "\n")
		announcementVal = strings.TrimSpace(raw)
	})
	return announcementVal
}

// announcementHash 对公告内容做 sha256 摘要（hex）。内容一变哈希即变，
// 前端据此判断「公告是否更新」：哈希不同才重新弹出。空公告返回空串。
func announcementHash() string {
	body := announcementText()
	if body == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// handleAnnouncementGet 轻量公告接口：纯本地读取，不做任何会话校验，
// 避免公告弹窗走 /api/config 时因 liveJMSession 触发一次 JM 上游网络往返。
func handleAnnouncementGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"announcement": announcementText(),
		"hash":         announcementHash(),
		"st":           StatusOK,
		"msg":          "",
	})
}

func handleConfigGet(w http.ResponseWriter, r *http.Request) {
	rawU, _ := siteUserOf(r)
	if strings.TrimSpace(rawU) == "" {
		rawU = "anon"
	}
	u := store.CredActiveUsername(rawU)
	isLoggedIn := liveJMSession(r)
	writeJSON(w, 200, map[string]any{
		"username":     u,
		"is_logged_in": isLoggedIn,
		"announcement": announcementText(),
		"st":           StatusOK,
		"msg":          "",
	})
}
