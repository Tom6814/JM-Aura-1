// Package bika 实现哔咔漫画（picacomic）API 客户端。
//
// 与 jm 包完全独立：签名机制（HMAC-SHA256 + 自定义头）、域名、登录方式都不同，
// 因此单独成包，避免互相污染，也验证了 provider 扩展点的价值。
package bika

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	apiKey    = "C69BAF41DA5ABD1FFEDC6D2FEA56B"
	secretKey = "~d}$Q7$eIni=V)9\\RK/P.RM4;9[7|@/CA}b~OW!3?EV`:<>M7pddUBL5n|0/*Cn"

	appVersion      = "2.2.1.3.3.4"
	appUUID         = "defaultUuid"
	appPlatform     = "android"
	appBuildVersion = "45"
	appChannel      = "3"
	userAgent       = "okhttp/3.8.1"
	imageQuality    = "original"
	acceptHeader    = "application/vnd.picacomic.com.v1+json"
)

// DefaultBaseURLs 为哔咔 API 线路，按顺序 failover。
var DefaultBaseURLs = []string{
	"https://picaapi.picacomic.com/",
	"https://picaapi.go2778.com/",
}

// ErrNeedLogin 表示上游判定登录失效（HTTP 401 / code 401 unauthorized）。
var ErrNeedLogin = errors.New("bika: 登录已过期")

// APIError 表示上游返回的业务错误。
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("bika: 上游错误(%d)", e.Code)
	}
	return e.Message
}

// Client 是哔咔 API 客户端。并发安全（无可变共享状态）。
type Client struct {
	http     *http.Client
	baseURLs []string
}

// NewClient 创建默认客户端（主/备线路 + 15s 超时）。
func NewClient() *Client {
	return &Client{
		http:     &http.Client{Timeout: 15 * time.Second},
		baseURLs: append([]string(nil), DefaultBaseURLs...),
	}
}

// SetBaseURLs 覆盖线路列表（空则忽略）。
func (c *Client) SetBaseURLs(urls []string) {
	if len(urls) == 0 {
		return
	}
	c.baseURLs = append([]string(nil), urls...)
}

func nonceHex() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// signature 复刻插件算法：HMAC_SHA256(secret, lower(path+ts+nonce+METHOD+apiKey))。
func signature(path string, ts int64, nonce, method string) string {
	raw := strings.ToLower(path + strconv.FormatInt(ts, 10) + nonce + method + apiKey)
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}

// Request 打上游并返回信封中的 data。token 为空则不带头 authorization。
func (c *Client) Request(ctx context.Context, method, p string, query url.Values, body any, token string) (any, error) {
	p = "/" + strings.TrimPrefix(p, "/")
	qs := ""
	if len(query) > 0 {
		qs = "?" + query.Encode()
	}
	sigPath := strings.TrimPrefix(p+qs, "/")

	var payload []byte
	hasBody := body != nil
	if hasBody {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = b
	}

	var lastErr error
	for _, base := range c.baseURLs {
		data, transportErr, err := c.doOnce(ctx, base, method, p, qs, sigPath, payload, hasBody, token)
		if err == nil {
			return data, nil
		}
		if !transportErr {
			return nil, err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("bika: 无可用线路")
	}
	return nil, lastErr
}

func (c *Client) doOnce(ctx context.Context, base, method, p, qs, sigPath string, payload []byte, hasBody bool, token string) (any, bool, error) {
	var rdr io.Reader
	if hasBody {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+strings.TrimPrefix(p, "/")+qs, rdr)
	if err != nil {
		return nil, false, err
	}
	ts := time.Now().Unix()
	non := nonceHex()
	req.Header.Set("api-key", apiKey)
	req.Header.Set("accept", acceptHeader)
	req.Header.Set("app-channel", appChannel)
	req.Header.Set("time", strconv.FormatInt(ts, 10))
	req.Header.Set("nonce", non)
	req.Header.Set("signature", signature(sigPath, ts, non, method))
	req.Header.Set("app-version", appVersion)
	req.Header.Set("app-uuid", appUUID)
	req.Header.Set("app-platform", appPlatform)
	req.Header.Set("app-build-version", appBuildVersion)
	req.Header.Set("user-agent", userAgent)
	req.Header.Set("image-quality", imageQuality)
	if hasBody {
		req.Header.Set("content-type", "application/json; charset=UTF-8")
	}
	if token != "" {
		req.Header.Set("authorization", token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, true, err
	}
	if res.StatusCode >= 500 {
		return nil, true, &APIError{Code: res.StatusCode, Message: fmt.Sprintf("bika: 上游 %d", res.StatusCode)}
	}
	var env map[string]any
	if uerr := json.Unmarshal(raw, &env); uerr != nil {
		return nil, false, fmt.Errorf("bika: 响应解析失败: %w", uerr)
	}
	code := AsInt(env["code"])
	msg := AsStr(env["message"])
	if res.StatusCode == 401 || code == 401 {
		return nil, false, ErrNeedLogin
	}
	if code != 0 && code != 200 {
		return nil, false, &APIError{Code: code, Message: msg}
	}
	return env["data"], false, nil
}

// —— 取值辅助：上游字段类型不稳定，统一用这些稳健转换 ——

// AsInt 把任意数值/字符串转 int，失败返回 0。
func AsInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

// AsStr 把任意值转字符串。
func AsStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		if t {
			return "true"
		}
		return "false"
	}
	return fmt.Sprint(v)
}

// AsBool 稳健转 bool（支持字符串 true/false）。
func AsBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true")
	case float64:
		return t != 0
	case int:
		return t != 0
	}
	return false
}

// AsMap 转 map，非对象返回 nil。
func AsMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// AsList 转切片，非数组返回 nil。
func AsList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

// AsStrList 把数组字段转为去空的字符串切片。
func AsStrList(v any) []string {
	l := AsList(v)
	out := make([]string, 0, len(l))
	for _, item := range l {
		if s := strings.TrimSpace(AsStr(item)); s != "" {
			out = append(out, s)
		}
	}
	return out
}
