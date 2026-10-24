package common

import (
	"sort"
	"sync"
	"time"
)

// inMemoryRateLimiterMaxKeysDefault 是内存限流器的键数量硬上限默认值。
const inMemoryRateLimiterMaxKeysDefault = 100000

// InMemoryRateLimiterMaxKeys 是内存限流器的键数量硬上限。
//
// 声明为变量而不是常量，便于测试调小后验证逐出逻辑；生产环境使用默认 100000，
// 避免未认证请求用海量 key 把内存撑爆。
var InMemoryRateLimiterMaxKeys = inMemoryRateLimiterMaxKeysDefault

type InMemoryRateLimiter struct {
	store              map[string]*[]int64
	mutex              sync.Mutex
	expirationDuration time.Duration
	// stopCh 用于停止后台清理 goroutine；Close 后置 nil。
	stopCh chan struct{}
}

func (l *InMemoryRateLimiter) Init(expirationDuration time.Duration) {
	if l.store == nil {
		l.mutex.Lock()
		if l.store == nil {
			l.store = make(map[string]*[]int64)
			l.expirationDuration = expirationDuration
			stopCh := make(chan struct{})
			l.stopCh = stopCh
			if expirationDuration > 0 {
				go l.clearExpiredItems(stopCh)
			}
		}
		l.mutex.Unlock()
	}
}

// Close 停止后台清理 goroutine。可重复调用（幂等）。
func (l *InMemoryRateLimiter) Close() {
	l.mutex.Lock()
	if l.stopCh != nil {
		close(l.stopCh)
		l.stopCh = nil
	}
	l.mutex.Unlock()
}

func (l *InMemoryRateLimiter) clearExpiredItems(stopCh <-chan struct{}) {
	ticker := time.NewTicker(l.expirationDuration)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			l.mutex.Lock()
			now := time.Now().Unix()
			for key := range l.store {
				queue := l.store[key]
				size := len(*queue)
				if size == 0 || now-(*queue)[size-1] > int64(l.expirationDuration.Seconds()) {
					delete(l.store, key)
				}
			}
			l.mutex.Unlock()
		}
	}
}

// Size 返回当前键数量（测试钩子）。
func (l *InMemoryRateLimiter) Size() int {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return len(l.store)
}

// evictIfFullLocked 在键数量达到上限时清理过期键；仍超限则按"最后活跃时间"
// 批量逐出最旧键（至少腾出 10% 名额），保证内存占用有硬上限。
// 调用方必须持有 l.mutex。
func (l *InMemoryRateLimiter) evictIfFullLocked(now int64) {
	maxKeys := InMemoryRateLimiterMaxKeys
	if maxKeys <= 0 {
		maxKeys = inMemoryRateLimiterMaxKeysDefault
	}
	if len(l.store) < maxKeys {
		return
	}
	expiration := int64(l.expirationDuration.Seconds())
	// 先清理过期键（判定与 clearExpiredItems 一致）。
	for key, queue := range l.store {
		if queue == nil || len(*queue) == 0 {
			delete(l.store, key)
			continue
		}
		if expiration > 0 && now-(*queue)[len(*queue)-1] > expiration {
			delete(l.store, key)
		}
	}
	if len(l.store) < maxKeys {
		return
	}
	// 仍然超限：按最后活跃时间升序批量逐出最旧键。
	type keyActivity struct {
		key  string
		last int64
	}
	entries := make([]keyActivity, 0, len(l.store))
	for key, queue := range l.store {
		last := int64(0)
		if queue != nil && len(*queue) > 0 {
			last = (*queue)[len(*queue)-1]
		}
		entries = append(entries, keyActivity{key: key, last: last})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].last < entries[j].last })
	// 每次至少腾出 1 个位置：maxKeys<10 时 maxKeys/10 为 0，若直接用会导致永不逐出。
	step := maxKeys / 10
	if step < 1 {
		step = 1
	}
	target := maxKeys - step
	if target < 0 {
		target = 0
	}
	for i := 0; i < len(entries) && len(l.store) > target; i++ {
		delete(l.store, entries[i].key)
	}
}

// Delete 清除某个 key 的计数（例如认证成功后解除账号锁定计数）。
func (l *InMemoryRateLimiter) Delete(key string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	delete(l.store, key)
}

// Peek 判断 key 是否已达上限，但**不**记录本次请求。
//
// 用于"先检查锁定状态、只在失败时计数"的场景（例如账号级登录锁定）：
// 直接调用 Request 会把失败次数与合法尝试混在一起统计。
func (l *InMemoryRateLimiter) Peek(key string, maxRequestNum int, duration int64) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	queue, ok := l.store[key]
	if !ok || queue == nil || len(*queue) == 0 {
		return true
	}
	if len(*queue) < maxRequestNum {
		return true
	}
	now := time.Now().Unix()
	// 最旧的一条已滑出时间窗口，视为未锁定
	return now-(*queue)[0] >= duration
}

// Request parameter duration's unit is seconds
func (l *InMemoryRateLimiter) Request(key string, maxRequestNum int, duration int64) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	// [old <-- new]
	queue, ok := l.store[key]
	now := time.Now().Unix()
	if ok {
		if len(*queue) < maxRequestNum {
			*queue = append(*queue, now)
			return true
		} else {
			if now-(*queue)[0] >= duration {
				*queue = (*queue)[1:]
				*queue = append(*queue, now)
				return true
			} else {
				return false
			}
		}
	} else {
		l.evictIfFullLocked(now)
		s := make([]int64, 0, maxRequestNum)
		l.store[key] = &s
		*(l.store[key]) = append(*(l.store[key]), now)
	}
	return true
}
