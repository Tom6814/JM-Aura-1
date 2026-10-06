package store

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"
)

// bika_credentials.json：与 credentials.json（JM 专用）完全分离，避免互相污染。
//
//	{
//	  "v": 1,
//	  "users": {
//	    "alice": {
//	      "active": "a@b.com",
//	      "accounts": {
//	        "a@b.com": { "password_plain": "...", "token": "...", "token_at": 1727300000 }
//	      }
//	    }
//	  }
//	}

func bikaCredFile() string {
	if v := os.Getenv("JM_AURA_BIKA_CREDENTIALS_PATH"); v != "" {
		return v
	}
	return "bika_credentials.json"
}

func loadBikaCredDoc() map[string]any {
	if doc, ok := LoadJSON(bikaCredFile()); ok {
		if _, ok := doc["users"].(map[string]any); !ok {
			doc["users"] = map[string]any{}
		}
		doc["v"] = 1
		return doc
	}
	return map[string]any{"v": 1, "users": map[string]any{}}
}

func saveBikaCredDoc(doc map[string]any) {
	doc["v"] = 1
	_ = SaveJSON(bikaCredFile(), doc)
}

func bikaBucket(doc map[string]any, siteUser string) map[string]any {
	users, _ := doc["users"].(map[string]any)
	if users == nil {
		users = map[string]any{}
		doc["users"] = users
	}
	k := strings.TrimSpace(siteUser)
	if k == "" {
		k = "anon"
	}
	b, _ := users[k].(map[string]any)
	if b == nil {
		b = map[string]any{}
		users[k] = b
	}
	return b
}

func bikaAccounts(b map[string]any) (map[string]any, string) {
	acc, _ := b["accounts"].(map[string]any)
	if acc == nil {
		acc = map[string]any{}
		b["accounts"] = acc
	}
	active := strings.TrimSpace(getStr(b, "active"))
	return acc, active
}

// BikaCredSet 写入/更新一个 bika 账号（邮箱+密码+token），并设为当前激活账号。
func BikaCredSet(siteUser, email, password, token string) error {
	u := strings.TrimSpace(email)
	if u == "" || password == "" {
		return errors.New("Missing email or password")
	}
	doc := loadBikaCredDoc()
	b := bikaBucket(doc, siteUser)
	acc, _ := bikaAccounts(b)
	rec, _ := acc[u].(map[string]any)
	if rec == nil {
		rec = map[string]any{}
		acc[u] = rec
	}
	rec["password_plain"] = password
	rec["password_keyring"] = false
	rec["token"] = token
	rec["token_at"] = time.Now().Unix()
	delete(rec, "password_dpapi_b64")
	b["active"] = u
	saveBikaCredDoc(doc)
	return nil
}

// BikaCredSetToken 仅刷新某账号的 token（自动重登后回写）。
func BikaCredSetToken(siteUser, email, token string) {
	u := strings.TrimSpace(email)
	if u == "" {
		return
	}
	doc := loadBikaCredDoc()
	b := bikaBucket(doc, siteUser)
	acc, _ := bikaAccounts(b)
	rec, _ := acc[u].(map[string]any)
	if rec == nil {
		rec = map[string]any{}
		acc[u] = rec
	}
	rec["token"] = token
	rec["token_at"] = time.Now().Unix()
	saveBikaCredDoc(doc)
}

// BikaCredActive 返回当前激活的 bika 账号名；仅当该账号持有 token 时才算「已登录」。
func BikaCredActive(siteUser string) string {
	doc := loadBikaCredDoc()
	b := bikaBucket(doc, siteUser)
	acc, active := bikaAccounts(b)
	u := active
	if u == "" {
		return ""
	}
	rec, _ := acc[u].(map[string]any)
	if rec == nil {
		return ""
	}
	if strings.TrimSpace(getStr(rec, "token")) == "" {
		return ""
	}
	return u
}

// BikaCredLogin 返回激活账号的 (email, password, token)，供 token 失效时自动重登。
func BikaCredLogin(siteUser string) (string, string, string) {
	doc := loadBikaCredDoc()
	b := bikaBucket(doc, siteUser)
	acc, active := bikaAccounts(b)
	u := active
	if u == "" {
		keys := make([]string, 0, len(acc))
		for k := range acc {
			if strings.TrimSpace(k) != "" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			return "", "", ""
		}
		u = keys[0]
	}
	rec, _ := acc[u].(map[string]any)
	if rec == nil {
		return "", "", ""
	}
	return u, getStr(rec, "password_plain"), getStr(rec, "token")
}

// BikaCredList 列出某站点账号下的 bika 账号。
func BikaCredList(siteUser string) CredList {
	doc := loadBikaCredDoc()
	b := bikaBucket(doc, siteUser)
	acc, active := bikaAccounts(b)
	names := make([]string, 0, len(acc))
	for k := range acc {
		if u := strings.TrimSpace(k); u != "" {
			names = append(names, u)
		}
	}
	sort.Strings(names)
	out := make([]CredAccount, 0, len(names))
	for _, u := range names {
		rec, _ := acc[u].(map[string]any)
		out = append(out, CredAccount{
			Username:    u,
			Active:      u == active,
			HasPassword: credHasPassword(rec),
		})
	}
	return CredList{Active: active, Accounts: out}
}

// BikaCredRemove 删除某 bika 账号，必要时切换 active。
func BikaCredRemove(siteUser, email string) error {
	u := strings.TrimSpace(email)
	if u == "" {
		return errors.New("Missing email")
	}
	doc := loadBikaCredDoc()
	b := bikaBucket(doc, siteUser)
	acc, active := bikaAccounts(b)
	delete(acc, u)
	if active == u {
		keys := make([]string, 0, len(acc))
		for k := range acc {
			if strings.TrimSpace(k) != "" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			b["active"] = keys[0]
		} else {
			delete(b, "active")
		}
	}
	saveBikaCredDoc(doc)
	return nil
}

// BikaCredClear 清空某站点账号下的全部 bika 账号。
func BikaCredClear(siteUser string) error {
	doc := loadBikaCredDoc()
	b := bikaBucket(doc, siteUser)
	b["accounts"] = map[string]any{}
	delete(b, "active")
	saveBikaCredDoc(doc)
	return nil
}
