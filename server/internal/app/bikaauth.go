package app

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"jmaura/internal/bika"
	"jmaura/internal/store"
)

// bikaClient 是全局单例：无状态、并发安全。
var bikaClient = bika.NewClient()

func init() {
	if raw := strings.TrimSpace(os.Getenv("BIKA_API_BASES")); raw != "" {
		var urls []string
		for _, p := range strings.Split(raw, ",") {
			if s := strings.TrimSpace(p); s != "" {
				urls = append(urls, s)
			}
		}
		bikaClient.SetBaseURLs(urls)
	}
}

// —— 公共（匿名）账号 ——
//
// 仅在环境变量提供凭据时启用，用于「未登录用户」的只读浏览。
// 硬约束：它绝不作为「当前用户已登录」对外暴露——profile 始终按用户自身判断，
// 写操作（收藏/评论/签到）也绝不用公共账号代写。

var (
	bikaPublicMu    sync.Mutex
	bikaPublicToken string
)

func bikaPublicCreds() (string, string) {
	return strings.TrimSpace(os.Getenv("BIKA_ACCOUNT")), os.Getenv("BIKA_PASSWORD")
}

// BikaPublicConfigured 报告是否配置了公共账号（供 /capabilities 展示）。
func BikaPublicConfigured() bool {
	e, p := bikaPublicCreds()
	return e != "" && p != ""
}

// bikaPublicTokenGet 返回公共账号 token；force=true 时强制重新登录。
func bikaPublicTokenGet(ctx context.Context, force bool) string {
	email, pw := bikaPublicCreds()
	if email == "" || pw == "" {
		return ""
	}
	bikaPublicMu.Lock()
	defer bikaPublicMu.Unlock()
	if !force && bikaPublicToken != "" {
		return bikaPublicToken
	}
	tok, err := bikaClient.SignIn(ctx, email, pw)
	if err != nil {
		bikaPublicToken = ""
		return ""
	}
	bikaPublicToken = tok
	return tok
}

const bikaSourceTag = "#bika#"

// bikaSiteUserOf 从隔离键解析站点账号（仅当该键带 #bika# 段，即用户本人已登录 bika）。
func bikaSiteUserOf(identity string) (string, bool) {
	if i := strings.Index(identity, bikaSourceTag); i > 0 {
		return identity[:i], true
	}
	return "", false
}

// bikaUserLoggedIn 报告当前请求身份是否属于「用户本人已登录 bika」。
func bikaUserLoggedIn(identity string) bool {
	_, ok := bikaSiteUserOf(identity)
	return ok
}

// bikaTokenFor 解析本次请求应使用的 token：
// 用户已登录 → 用户 token（失效时用保存的密码重登）；否则 → 借公共账号（只读）。
func bikaTokenFor(ctx context.Context, identity string) string {
	if siteUser, ok := bikaSiteUserOf(identity); ok {
		email, pw, tok := store.BikaCredLogin(siteUser)
		if tok != "" {
			return tok
		}
		if email != "" && pw != "" {
			if t, err := bikaClient.SignIn(ctx, email, pw); err == nil {
				store.BikaCredSetToken(siteUser, email, t)
				return t
			}
		}
	}
	return bikaPublicTokenGet(ctx, false)
}

// bikaRefreshToken 强制刷新指定身份要用的 token：用户身份用保存的密码重登，
// 否则强制刷新公共账号。任何一步失败都返回空串（调用方据此判断无法恢复）。
func bikaRefreshToken(ctx context.Context, identity string) string {
	if siteUser, ok := bikaSiteUserOf(identity); ok {
		email, pw, _ := store.BikaCredLogin(siteUser)
		if email != "" && pw != "" {
			if t, err := bikaClient.SignIn(ctx, email, pw); err == nil {
				store.BikaCredSetToken(siteUser, email, t)
				return t
			}
		}
		return ""
	}
	return bikaPublicTokenGet(ctx, true)
}

// bikaCall 执行一次上游调用；命中登录失效时按身份刷新 token 后重试一次。
func bikaCall(ctx context.Context, identity string, fn func(token string) (any, error)) (any, error) {
	data, err := fn(bikaTokenFor(ctx, identity))
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, bika.ErrNeedLogin) {
		return nil, err
	}
	if t := bikaRefreshToken(ctx, identity); t != "" {
		return fn(t)
	}
	return nil, err
}

// bikaUserLogin 用用户自己的 bika 邮箱+密码登录，并把凭据绑定到当前站点账号。
func bikaUserLogin(ctx context.Context, siteUser, email, password string) (any, error) {
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return nil, v2ProviderErr("账号或密码不能为空", 400)
	}
	token, err := bikaClient.SignIn(ctx, email, password)
	if err != nil {
		return nil, err
	}
	if serr := store.BikaCredSet(siteUser, email, password, token); serr != nil {
		return nil, serr
	}
	return bikaClient.Profile(ctx, token)
}

// bikaUserLogout 清除当前站点账号绑定的 bika 凭据。
func bikaUserLogout(siteUser string) error {
	return store.BikaCredClear(siteUser)
}

// bikaRequireUser 写操作前置：要求用户本人已登录 bika。
func bikaRequireUser(identity string) error {
	if !bikaUserLoggedIn(identity) {
		return v2ProviderErr("请先登录哔咔账号", 401)
	}
	return nil
}

// isBikaNeedLogin 报告错误是否为 bika 登录失效（供 v2Fail 统一映射）。
func isBikaNeedLogin(err error) bool { return errors.Is(err, bika.ErrNeedLogin) }

const bikaRandAlpha = "abcdefghijklmnopqrstuvwxyz0123456789"

// bikaRandomToken 生成 n 位随机小写字母数字串（crypto/rand）。
func bikaRandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = bikaRandAlpha[int(v)%len(bikaRandAlpha)]
	}
	return string(out), nil
}

// bikaNewAccountFields 生成一组随机账号字段（对齐 manhuabika.com 一键注册的观感）：
// 同一个随机 base 派生昵称/邮箱/密码，生日取 20 年前（满足 ≥18 岁），性别用 "bot"。
func bikaNewAccountFields() (email, name, password string, params bika.RegisterParams, err error) {
	tok := func(n int) (string, error) { return bikaRandomToken(n) }
	base, err := tok(7)
	if err != nil {
		return "", "", "", params, err
	}
	q1, err := tok(6)
	if err != nil {
		return "", "", "", params, err
	}
	a1, err := tok(9)
	if err != nil {
		return "", "", "", params, err
	}
	q2, err := tok(6)
	if err != nil {
		return "", "", "", params, err
	}
	a2, err := tok(9)
	if err != nil {
		return "", "", "", params, err
	}
	q3, err := tok(7)
	if err != nil {
		return "", "", "", params, err
	}
	a3, err := tok(8)
	if err != nil {
		return "", "", "", params, err
	}
	day := time.Now().Format("20060102")
	email = base + day + "u"
	password = base + day + "p"
	name = "web" + base + day
	params = bika.RegisterParams{
		Name: name, Email: email, Password: password,
		Question1: q1, Answer1: a1,
		Question2: q2, Answer2: a2,
		Question3: q3, Answer3: a3,
		Birthday: time.Now().AddDate(-20, 0, 0).Format("2006-01-02"),
		Gender:   "bot",
	}
	return email, name, password, params, nil
}
