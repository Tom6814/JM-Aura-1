package app

import (
	"net/http"
	"sync"
	"time"
)

// 在线人数统计——刻意做得极轻量。
//
// 思路：每个已解析身份的请求，把「访客标识 → 最后活跃时间」写进一个内存 map；
// 统计时只数时间窗内仍活跃的标识个数。既不落盘、也不起后台 goroutine，
// 热路径上只多一次 map 写入 + 每两分钟一次的顺带清理，开销可忽略。
//
// 访客标识取自中间件已算出的身份（登录名，或游客的 g:<gid>），因此同一浏览器的
// 多个标签页只算一个人。

const (
	onlineWindowSecs = 300 // 5 分钟内有请求即视为在线
	onlineSweepSecs  = 120 // 顺带清理过期条目的最小间隔
)

var (
	onlineMu        sync.Mutex
	onlineSeen      = map[string]int64{}
	onlineLastSweep int64
)

// onlineTouch 记录一次活跃。仅在中间件已解析出身份的请求上调用。
func onlineTouch(identity string) {
	if identity == "" {
		return
	}
	now := time.Now().Unix()
	onlineMu.Lock()
	onlineSeen[identity] = now
	// 清理不是每次请求都做，避免热路径上的 O(n) 扫描。
	if now-onlineLastSweep >= onlineSweepSecs {
		cutoff := now - onlineWindowSecs
		for k, t := range onlineSeen {
			if t < cutoff {
				delete(onlineSeen, k)
			}
		}
		onlineLastSweep = now
	}
	onlineMu.Unlock()
}

// onlineCount 统计时间窗内仍活跃的访客数。
func onlineCount() int {
	cutoff := time.Now().Unix() - onlineWindowSecs
	onlineMu.Lock()
	defer onlineMu.Unlock()
	n := 0
	for _, t := range onlineSeen {
		if t >= cutoff {
			n++
		}
	}
	return n
}

// handleSiteOnline 返回当前在线人数，供页脚展示。
func handleSiteOnline(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, ok(map[string]any{"online": onlineCount()}, ""))
}
