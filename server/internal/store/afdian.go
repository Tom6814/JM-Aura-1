package store

import (
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

// 爱发电赞助绑定：站点账号 ←→ 爱发电订单号。
//
// 约定：
//   - 一单一账号：同一个订单号只能被一个站点账号绑定，防止订单号被分享。
//   - 永久有效：绑定后不随订单新旧失效（无需重复赞助）。
//   - 一个账号只保留一条绑定，换绑新订单会覆盖旧绑定。
//
// 文件形如 {"v":1,"orders":{"<订单号>":{"u":"<账号>","at":时间戳,"amt":"金额"}}}。

func afdianBindingsFile() string {
	if v := os.Getenv("JM_AURA_AFDIAN_BINDINGS_PATH"); v != "" {
		return v
	}
	return "afdian_bindings.json"
}

var (
	ErrAfdianOrderTaken   = errors.New("afdian order already bound to another account")
	ErrAfdianOrderInvalid = errors.New("invalid afdian order number")

	afdianBindMu sync.Mutex
)

// AfdianBinding 是一条已生效的赞助绑定。
type AfdianBinding struct {
	Username string
	OrderNo  string
	BoundAt  int64
	Amount   string
}

func loadAfdianOrders() map[string]any {
	doc, _ := LoadJSON(afdianBindingsFile())
	orders, _ := doc["orders"].(map[string]any)
	if orders == nil {
		orders = map[string]any{}
	}
	return orders
}

func saveAfdianOrders(orders map[string]any) error {
	return SaveJSON(afdianBindingsFile(), map[string]any{"v": 1, "orders": orders})
}

// AfdianOrderOwner 返回该订单号当前绑定的站点账号；未绑定返回空串。
func AfdianOrderOwner(orderNo string) string {
	orderNo = normAfdianOrderNo(orderNo)
	if orderNo == "" {
		return ""
	}
	afdianBindMu.Lock()
	defer afdianBindMu.Unlock()
	rec, _ := loadAfdianOrders()[orderNo].(map[string]any)
	return getStr(rec, "u")
}

// AfdianBindingOf 返回某站点账号当前的绑定。
func AfdianBindingOf(username string) (AfdianBinding, bool) {
	username = NormUsername(username)
	if username == "" {
		return AfdianBinding{}, false
	}
	afdianBindMu.Lock()
	defer afdianBindMu.Unlock()
	for orderNo, v := range loadAfdianOrders() {
		rec, _ := v.(map[string]any)
		if getStr(rec, "u") != username {
			continue
		}
		at, _ := rec["at"].(float64)
		return AfdianBinding{
			Username: username,
			OrderNo:  orderNo,
			BoundAt:  int64(at),
			Amount:   getStr(rec, "amt"),
		}, true
	}
	return AfdianBinding{}, false
}

// HasAfdianBinding 表示该站点账号是否为已绑定订单的赞助用户。
func HasAfdianBinding(username string) bool {
	_, ok := AfdianBindingOf(username)
	return ok
}

// AfdianBind 把已验证通过的爱发电订单绑定到站点账号。
func AfdianBind(username, orderNo, amount string) (AfdianBinding, error) {
	username = NormUsername(username)
	orderNo = normAfdianOrderNo(orderNo)
	if username == "" || orderNo == "" {
		return AfdianBinding{}, ErrAfdianOrderInvalid
	}
	afdianBindMu.Lock()
	defer afdianBindMu.Unlock()
	orders := loadAfdianOrders()
	if rec, _ := orders[orderNo].(map[string]any); rec != nil && getStr(rec, "u") != username {
		return AfdianBinding{}, ErrAfdianOrderTaken
	}
	// 一个账号只保留一条：换绑时移除其旧订单。
	for k, v := range orders {
		rec, _ := v.(map[string]any)
		if getStr(rec, "u") == username {
			delete(orders, k)
		}
	}
	now := time.Now().Unix()
	rec := map[string]any{"u": username, "at": now}
	if amount != "" {
		rec["amt"] = amount
	}
	orders[orderNo] = rec
	if err := saveAfdianOrders(orders); err != nil {
		return AfdianBinding{}, err
	}
	return AfdianBinding{Username: username, OrderNo: orderNo, BoundAt: now, Amount: amount}, nil
}

// normAfdianOrderNo 归一化并校验爱发电订单号（纯数字，8–64 位）。
func normAfdianOrderNo(s string) string {
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
