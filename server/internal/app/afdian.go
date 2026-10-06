package app

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"jmaura/internal/store"
)

// 爱发电（afdian）开放接口：赞助者名单。
// 开发者后台：https://afdian.net/dashboard/dev（afdian.net 域名当前不可达，
// 实测 afdian.com / ifdian.net 存活，故默认走 afdian.com）。
// 签名算法：sign = md5(token + "params" + params_str + "ts" + ts + "user_id" + user_id)
// token 仅参与签名，不随请求体传输。
const afdianSponsorAPI = "https://afdian.com/api/open/query-sponsor"

const afdianCacheTTL = 10 * time.Minute

type afdianSponsor struct {
	Name        string `json:"name"`
	Avatar      string `json:"avatar"`
	Amount      string `json:"all_sum_amount"`
	LastPayTime int64  `json:"last_pay_time"`
	PlanName    string `json:"plan_name"`
}

type afdianEntry struct {
	exp  time.Time
	body []byte
}

var (
	afdianMu    sync.Mutex
	afdianCache = map[string]afdianEntry{}
)

func afdianCacheGet(key string) ([]byte, bool) {
	afdianMu.Lock()
	defer afdianMu.Unlock()
	e, has := afdianCache[key]
	if !has || time.Now().After(e.exp) {
		if has {
			delete(afdianCache, key)
		}
		return nil, false
	}
	out := make([]byte, len(e.body))
	copy(out, e.body)
	return out, true
}

func afdianCachePut(key string, body []byte) {
	cp := make([]byte, len(body))
	copy(cp, body)
	afdianMu.Lock()
	defer afdianMu.Unlock()
	afdianCache[key] = afdianEntry{exp: time.Now().Add(afdianCacheTTL), body: cp}
}

func afdianCredentials() (userID, token string) {
	return strings.TrimSpace(os.Getenv("JM_AURA_AFDIAN_USER_ID")), strings.TrimSpace(os.Getenv("JM_AURA_AFDIAN_TOKEN"))
}

func afdianSign(token, params string, ts int64, userID string) string {
	raw := token + "params" + params + "ts" + strconv.FormatInt(ts, 10) + "user_id" + userID
	sum := md5.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// handleAfdianSponsors 代理爱发电赞助者名单，避免把 API Token 暴露到前端。
// 未配置凭证时优雅降级为空名单（HTTP 200），前端据此展示引导文案。
func handleAfdianSponsors(w http.ResponseWriter, r *http.Request) {
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}

	userID, token := afdianCredentials()
	configured := userID != "" && token != ""

	payload := map[string]any{
		"configured":  configured,
		"sponsors":    []afdianSponsor{},
		"total_count": 0,
		"page":        page,
	}
	if !configured {
		writeJSON(w, 200, ok(payload, "未配置爱发电凭证"))
		return
	}

	cacheKey := "sponsors:" + strconv.Itoa(page)
	if body, has := afdianCacheGet(cacheKey); has {
		writeJSON(w, 200, json.RawMessage(body))
		return
	}

	sponsors, total, err := fetchAfdianSponsors(r.Context(), userID, token, page)
	if err != nil {
		payload["configured"] = false
		writeJSON(w, 200, ok(payload, "爱发电名单获取失败"))
		return
	}
	payload["sponsors"] = sponsors
	payload["total_count"] = total

	body, merr := json.Marshal(ok(payload, ""))
	if merr != nil {
		writeJSON(w, 200, ok(payload, ""))
		return
	}
	afdianCachePut(cacheKey, body)
	writeJSON(w, 200, json.RawMessage(body))
}

func fetchAfdianSponsors(ctx context.Context, userID, token string, page int) ([]afdianSponsor, int, error) {
	ts := time.Now().Unix()
	params := fmt.Sprintf(`{"page":%d}`, page)
	sign := afdianSign(token, params, ts, userID)

	reqBody, err := json.Marshal(map[string]any{
		"user_id": userID,
		"params":  params,
		"ts":      ts,
		"sign":    sign,
	})
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, afdianSponsorAPI, bytes.NewReader(reqBody))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", chrome120UA)

	resp, err := proxyClientVerify.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer drainClose(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("afdian HTTP %d", resp.StatusCode)
	}

	var parsed struct {
		EC   int    `json:"ec"`
		EM   string `json:"em"`
		Data struct {
			TotalCount int `json:"total_count"`
			TotalPage  int `json:"total_page"`
			List       []struct {
				AllSumAmount string `json:"all_sum_amount"`
				LastPayTime  int64  `json:"last_pay_time"`
				CurrentPlan  struct {
					Name string `json:"name"`
				} `json:"current_plan"`
				User struct {
					Name   string `json:"name"`
					Avatar string `json:"avatar"`
				} `json:"user"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, 0, err
	}
	if parsed.EC != 200 {
		return nil, 0, fmt.Errorf("afdian ec=%d em=%s", parsed.EC, parsed.EM)
	}

	out := make([]afdianSponsor, 0, len(parsed.Data.List))
	for _, it := range parsed.Data.List {
		name := strings.TrimSpace(it.User.Name)
		if name == "" {
			name = "匿名赞助者"
		}
		out = append(out, afdianSponsor{
			Name:        name,
			Avatar:      strings.TrimSpace(it.User.Avatar),
			Amount:      it.AllSumAmount,
			LastPayTime: it.LastPayTime,
			PlanName:    strings.TrimSpace(it.CurrentPlan.Name),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastPayTime > out[j].LastPayTime })
	return out, parsed.Data.TotalCount, nil
}

// —— 赞助订单校验与绑定 ——
//
// 用户在「设置」页填入在爱发电的订单号，后端调用 query-order 核验该订单确实存在且为
// 已支付（status=2），核验通过才把订单号绑定到站点账号，作为「无损下载」的准入条件。
// 一单一账号、永久有效；判定逻辑见 store.AfdianBind。

const afdianOrderAPI = "https://afdian.com/api/open/query-order"

// 回退扫描的页数上限（每页 50 条）。直查命中时不会走到这里。
const afdianOrderScanPages = 20

// afdianBindCooldown 限制同一账号的绑定请求频率：绑定未命中时服务端最多会翻
// afdianOrderScanPages 页平台订单，不设限等于给了外部一个请求放大器。
const afdianBindCooldown = 20 * time.Second

// afdianBindAttempts 记录每个账号上次发起校验的时间（username -> time.Time）。
var afdianBindAttempts sync.Map

type afdianOrder struct {
	OutTradeNo string
	Amount     string
	Status     int
	CreateTime int64
}

type afdianOrderItem struct {
	OutTradeNo  string `json:"out_trade_no"`
	TotalAmount string `json:"total_amount"`
	Status      int    `json:"status"`
	CreateTime  int64  `json:"create_time"`
}

type afdianOrderListResponse struct {
	EC   int    `json:"ec"`
	EM   string `json:"em"`
	Data struct {
		TotalPage int               `json:"total_page"`
		List      []afdianOrderItem `json:"list"`
	} `json:"data"`
}

func afdianConfigured() bool {
	userID, token := afdianCredentials()
	return userID != "" && token != ""
}

// normalizeAfdianOrderNo 归一化并校验订单号形态（爱发电 out_trade_no 为纯数字串）。
func normalizeAfdianOrderNo(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 8 || len(s) > 64 {
		return ""
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return ""
		}
	}
	return s
}

// afdianQueryOrders 调用 query-order。params 为接口参数的 JSON 字符串。
func afdianQueryOrders(ctx context.Context, userID, token, params string) (afdianOrderListResponse, error) {
	var out afdianOrderListResponse
	ts := time.Now().Unix()
	reqBody, err := json.Marshal(map[string]any{
		"user_id": userID,
		"params":  params,
		"ts":      ts,
		"sign":    afdianSign(token, params, ts, userID),
	})
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, afdianOrderAPI, bytes.NewReader(reqBody))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", chrome120UA)

	resp, err := proxyClientVerify.Do(req)
	if err != nil {
		return out, err
	}
	defer drainClose(resp)
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("afdian HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	if out.EC != 200 {
		return out, fmt.Errorf("afdian ec=%d em=%s", out.EC, out.EM)
	}
	return out, nil
}

func afdianOrderFromItem(it afdianOrderItem) afdianOrder {
	return afdianOrder{OutTradeNo: it.OutTradeNo, Amount: it.TotalAmount, Status: it.Status, CreateTime: it.CreateTime}
}

func findAfdianOrder(page afdianOrderListResponse, orderNo string) (afdianOrder, bool) {
	for _, it := range page.Data.List {
		if it.OutTradeNo == orderNo {
			return afdianOrderFromItem(it), true
		}
	}
	return afdianOrder{}, false
}

// fetchAfdianOrder 按订单号查询。先尝试用 out_trade_no 直查（平台支持时一步命中）；
// 未命中则回退为按页倒序扫描（有页数上限），避免平台忽略该参数时误判为「订单不存在」。
func fetchAfdianOrder(ctx context.Context, userID, token, orderNo string) (afdianOrder, bool, error) {
	// 先按订单号直查。query-order 的 out_trade_no 参数并非所有平台版本都生效：
	// 生效时直接命中；不生效时返回的是第一页，那就把它当作扫描的第一页复用，
	// 避免为了同一页数据再多打一次平台接口。
	var firstPage *afdianOrderListResponse
	if page, err := afdianQueryOrders(ctx, userID, token, fmt.Sprintf(`{"out_trade_no":%q}`, orderNo)); err == nil {
		if it, found := findAfdianOrder(page, orderNo); found {
			return it, true, nil
		}
		if len(page.Data.List) > 0 {
			firstPage = &page
		}
	}
	for p := 1; p <= afdianOrderScanPages; p++ {
		var page afdianOrderListResponse
		if p == 1 && firstPage != nil {
			page = *firstPage
		} else {
			fetched, err := afdianQueryOrders(ctx, userID, token, fmt.Sprintf(`{"page":%d}`, p))
			if err != nil {
				return afdianOrder{}, false, err
			}
			page = fetched
		}
		if it, found := findAfdianOrder(page, orderNo); found {
			return it, true, nil
		}
		if len(page.Data.List) == 0 || (page.Data.TotalPage > 0 && p >= page.Data.TotalPage) {
			break
		}
	}
	return afdianOrder{}, false, nil
}

func afdianBindingPayload(username string) map[string]any {
	payload := map[string]any{
		"configured": afdianConfigured(),
		"donor":      false,
	}
	if b, ok := store.AfdianBindingOf(username); ok {
		payload["donor"] = true
		payload["order_no"] = b.OrderNo
		payload["bound_at"] = b.BoundAt
		if b.Amount != "" {
			payload["amount"] = b.Amount
		}
	}
	return payload
}

// handleAfdianBindingGet 返回当前登录账号的赞助绑定状态（供下载面板与设置页使用）。
func handleAfdianBindingGet(w http.ResponseWriter, r *http.Request) {
	u, okc := requireSiteUser(w, r)
	if !okc {
		return
	}
	writeJSON(w, 200, ok(afdianBindingPayload(u), ""))
}

// handleAfdianBindingPost 校验爱发电订单号并绑定到当前登录账号。
func handleAfdianBindingPost(w http.ResponseWriter, r *http.Request) {
	u, okc := requireSiteUser(w, r)
	if !okc {
		return
	}
	var req struct {
		OrderNo string `json:"order_no"`
	}
	// 请求体只是一个订单号，限制大小，避免异常大包占用内存。
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 200, errSt(StatusUserError, "请求格式不正确"))
		return
	}
	orderNo := normalizeAfdianOrderNo(req.OrderNo)
	if orderNo == "" {
		writeJSON(w, 200, errSt(StatusUserError, "订单号格式不正确，请填写纯数字的订单号"))
		return
	}
	if owner := store.AfdianOrderOwner(orderNo); owner != "" && owner != u {
		writeJSON(w, 200, errSt(StatusUserError, "该订单号已被其他账号绑定"))
		return
	}
	userID, token := afdianCredentials()
	if userID == "" || token == "" {
		writeJSON(w, 200, errSt(StatusError, "本站尚未配置爱发电校验凭证，请联系管理员"))
		return
	}
	// 限流：下面这次校验最多会翻 20 页平台订单，防止被反复提交放大成批量外部请求。
	if last, seen := afdianBindAttempts.Load(u); seen {
		if since := time.Since(last.(time.Time)); since < afdianBindCooldown {
			wait := int((afdianBindCooldown-since)/time.Second) + 1
			writeJSON(w, 200, errSt(StatusUserError, fmt.Sprintf("操作过于频繁，请 %d 秒后再试", wait)))
			return
		}
	}
	afdianBindAttempts.Store(u, time.Now())
	order, found, err := fetchAfdianOrder(r.Context(), userID, token, orderNo)
	if err != nil {
		writeJSON(w, 200, errSt(StatusNetError, "爱发电校验失败，请稍后重试"))
		return
	}
	if !found {
		writeJSON(w, 200, errSt(StatusUserError, "未查询到该订单，请核对订单号（爱发电「我的」→「订单」）"))
		return
	}
	if order.Status != 2 {
		writeJSON(w, 200, errSt(StatusUserError, "该订单尚未支付成功，暂不能用于绑定"))
		return
	}
	b, berr := store.AfdianBind(u, orderNo, order.Amount)
	if berr != nil {
		if berr == store.ErrAfdianOrderTaken {
			writeJSON(w, 200, errSt(StatusUserError, "该订单号已被其他账号绑定"))
			return
		}
		writeJSON(w, 200, errSt(StatusSaveError, "绑定失败，请稍后重试"))
		return
	}
	writeJSON(w, 200, ok(map[string]any{
		"donor":    true,
		"order_no": b.OrderNo,
		"bound_at": b.BoundAt,
		"amount":   b.Amount,
	}, "绑定成功，已解锁无损下载"))
}
