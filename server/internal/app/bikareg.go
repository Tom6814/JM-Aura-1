package app

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"jmaura/internal/store"
)

// 一键注册后台任务：POST 立即返回任务快照，实际建号/登录/绑定在后台 goroutine 里跑，
// 前端只需低频轮询状态。这样前端不承担重活，用户刷新/关弹窗也不会中断注册。
const (
	bikaRegMaxAttempts = 4
	bikaRegTaskTTL     = 15 * time.Minute
	bikaRegBackoff     = 1500 * time.Millisecond
)

type bikaRegisterTask struct {
	ID          string `json:"task_id"`
	State       string `json:"state"` // running | done | error
	Step        string `json:"step"`
	Attempt     int    `json:"attempt"`
	MaxAttempts int    `json:"max_attempts"`
	Account     string `json:"account,omitempty"`
	Name        string `json:"name,omitempty"`
	Profile     any    `json:"profile,omitempty"`
	Error       string `json:"error,omitempty"`

	owner   string
	updated time.Time
}

var (
	bikaRegMu    sync.Mutex
	bikaRegTasks = map[string]*bikaRegisterTask{}
)

// bikaRegCopy 在锁内拷贝，避免把会被后台并发改写的结构体直接交给 JSON 编码（数据竞争）。
func bikaRegCopyLocked(t *bikaRegisterTask) *bikaRegisterTask {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

func bikaRegPurgeLocked(now time.Time) {
	for id, t := range bikaRegTasks {
		if now.Sub(t.updated) > bikaRegTaskTTL {
			delete(bikaRegTasks, id)
		}
	}
}

// bikaRegisterStart 启动注册任务；幂等（连点/重复调用都不会重复建号）：
//   - 已有在跑的任务 → 直接复用（上游注册有频控，重复建号代价高）
//   - 已绑定 → 直接返回「已完成」快照
//
// 判断与插入都在同一把锁内完成，避免并发下出现两个任务。
func bikaRegisterStart(owner string) *bikaRegisterTask {
	bikaRegMu.Lock()
	bikaRegPurgeLocked(time.Now())
	for _, t := range bikaRegTasks {
		if t.owner == owner && t.State == "running" {
			c := bikaRegCopyLocked(t)
			bikaRegMu.Unlock()
			return c
		}
	}
	if email := store.BikaCredActive(owner); email != "" {
		bikaRegMu.Unlock()
		return &bikaRegisterTask{State: "done", Step: "已完成绑定", Account: email}
	}
	id, _ := bikaRandomToken(16)
	t := &bikaRegisterTask{
		ID:          id,
		State:       "running",
		Step:        "正在创建哔咔账号…",
		MaxAttempts: bikaRegMaxAttempts,
		owner:       owner,
		updated:     time.Now(),
	}
	bikaRegTasks[id] = t
	c := bikaRegCopyLocked(t)
	bikaRegMu.Unlock()

	go bikaRegisterRun(id, owner)
	return c
}

func bikaRegisterGet(owner, id string) *bikaRegisterTask {
	bikaRegMu.Lock()
	defer bikaRegMu.Unlock()
	bikaRegPurgeLocked(time.Now())
	t := bikaRegTasks[id]
	if t == nil || t.owner != owner {
		return nil
	}
	return bikaRegCopyLocked(t)
}

func bikaRegUpdate(id string, fn func(*bikaRegisterTask)) {
	bikaRegMu.Lock()
	defer bikaRegMu.Unlock()
	if t := bikaRegTasks[id]; t != nil {
		fn(t)
		t.updated = time.Now()
	}
}

// bikaRegisterRun 后台执行：建号 → 立即登录 → 绑定到站点账号。
// 上游注册有短窗频控（返回误导性的 "name is already exist"），这里按次数退避重试，
// 且每次都用全新随机字段（随机字段不可能真重名，重试安全）。
func bikaRegisterRun(id, owner string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= bikaRegMaxAttempts; attempt++ {
		if attempt > 1 {
			bikaRegUpdate(id, func(t *bikaRegisterTask) {
				t.Step = "上游限流，正在重试…"
				t.Attempt = attempt
			})
			// 仅 ctx 取消才退出循环；上一轮的 lastErr 不能作为退出条件。
			var ctxErr error
			select {
			case <-ctx.Done():
				ctxErr = ctx.Err()
			case <-time.After(time.Duration(attempt) * bikaRegBackoff):
			}
			if ctxErr != nil {
				lastErr = ctxErr
				break
			}
		}

		email, name, password, params, ferr := bikaNewAccountFields()
		if ferr != nil {
			lastErr = ferr
			break
		}

		bikaRegUpdate(id, func(t *bikaRegisterTask) {
			t.Step = "正在创建哔咔账号…"
			t.Attempt = attempt
		})
		if rerr := bikaClient.Register(ctx, params); rerr != nil {
			lastErr = rerr
			continue
		}

		bikaRegUpdate(id, func(t *bikaRegisterTask) { t.Step = "正在登录新账号…" })
		token, serr := bikaClient.SignIn(ctx, email, password)
		if serr != nil {
			lastErr = serr
			continue
		}

		bikaRegUpdate(id, func(t *bikaRegisterTask) { t.Step = "正在绑定到当前账号…" })
		if werr := store.BikaCredSet(owner, email, password, token); werr != nil {
			lastErr = werr
			break
		}

		// 资料仅作展示，拉取失败不影响结果。
		profile, _ := bikaProfileData(ctx, owner+bikaSourceTag+email)
		bikaRegUpdate(id, func(t *bikaRegisterTask) {
			t.State = "done"
			t.Step = "已完成绑定"
			t.Account = email
			t.Name = name
			t.Profile = profile
		})
		return
	}

	if lastErr == nil {
		lastErr = errors.New("bika: 注册无可用响应")
	}
	// 上游被限流时返回误导性文案，直接透给用户会困惑：日志留真相、界面给可操作提示。
	log.Printf("bika register task %s failed after %d attempts: %v", id, bikaRegMaxAttempts, lastErr)
	bikaRegUpdate(id, func(t *bikaRegisterTask) {
		t.State = "error"
		t.Step = ""
		t.Error = "哔咔注册失败（上游可能限流），请稍后重试"
	})
}
