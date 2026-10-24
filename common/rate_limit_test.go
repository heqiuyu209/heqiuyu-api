package common

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMaskEmail 覆盖 common.MaskEmail 的日志脱敏行为（R2 §5）。
//
// 注意：实现当前保留域名、把 local part 全部替换为 "***"，并**不**保留首字符
// （计划文字写的是"保留首字符与域名"，实际实现为 "***@domain"；本测试按实际行为
// 断言，差异已在验收报告 R2 残余风险中记录，未改产品代码）。
func TestMaskEmail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "***masked***"},
		{name: "no at sign", in: "not-an-email", want: "***masked***"},
		{name: "normal", in: "alice@example.com", want: "***@example.com"},
		{name: "mixed case domain kept", in: "Bob.Smith@Example.COM", want: "***@Example.COM"},
		{name: "local only masked", in: "a@b@c", want: "***@b@c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskEmail(tc.in); got != tc.want {
				t.Errorf("MaskEmail(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// 明文 local part 不得出现在脱敏结果里。
			if at := strings.Index(tc.in, "@"); at > 0 {
				local := tc.in[:at]
				if local != "" && strings.Contains(MaskEmail(tc.in), local) {
					t.Errorf("MaskEmail(%q) 泄露了明文 local part", tc.in)
				}
			}
		})
	}
}

// TestInMemoryRateLimiterDefaultMaxKeys 锁定内存限流器的硬上限默认值（R2 §7）。
func TestInMemoryRateLimiterDefaultMaxKeys(t *testing.T) {
	// 该变量可被测试临时调小，但初始值必须是 100000。
	if InMemoryRateLimiterMaxKeys != 100000 {
		t.Errorf("InMemoryRateLimiterMaxKeys 默认值应为 100000，实际 %d", InMemoryRateLimiterMaxKeys)
	}
	if inMemoryRateLimiterMaxKeysDefault != 100000 {
		t.Errorf("inMemoryRateLimiterMaxKeysDefault 应为 100000，实际 %d", inMemoryRateLimiterMaxKeysDefault)
	}
}

func t8LimiterKeyCount(l *InMemoryRateLimiter) int {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return len(l.store)
}

func t8LimiterHasKey(l *InMemoryRateLimiter, key string) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	_, ok := l.store[key]
	return ok
}

// TestInMemoryRateLimiterEvictsWhenFull 覆盖超上限逐出：不 panic、尺寸受控、
// 按最久未活跃逐出，新 key 仍可计数（R2 §7）。
func TestInMemoryRateLimiterEvictsWhenFull(t *testing.T) {
	prev := InMemoryRateLimiterMaxKeys
	InMemoryRateLimiterMaxKeys = 100
	t.Cleanup(func() { InMemoryRateLimiterMaxKeys = prev })

	limiter := &InMemoryRateLimiter{}
	limiter.Init(0) // expiration 0：不启动清理 goroutine、不做过期清理，聚焦逐出逻辑
	t.Cleanup(limiter.Close)

	// 直接填充 100 个已知"最后活跃时间"的 key（递增），避免依赖秒级时间精度。
	base := time.Now().Unix()
	limiter.mutex.Lock()
	for i := 0; i < 100; i++ {
		last := base + int64(i)
		limiter.store[fmt.Sprintf("old-%03d", i)] = &[]int64{last}
	}
	limiter.mutex.Unlock()

	// 新增 key 触发逐出：上限 100 → 先批量逐出最旧的 10%（old-000..009）。
	if !limiter.Request("new-key", 1, 60) {
		t.Fatalf("新增 key 的首次请求应放行")
	}

	if size := t8LimiterKeyCount(limiter); size > InMemoryRateLimiterMaxKeys {
		t.Errorf("逐出后 key 数量应 <= %d，实际 %d", InMemoryRateLimiterMaxKeys, size)
	}
	if t8LimiterHasKey(limiter, "old-000") {
		t.Errorf("最旧的 key old-000 应被逐出")
	}
	if !t8LimiterHasKey(limiter, "old-099") {
		t.Errorf("最新的 key old-099 不应被逐出")
	}
	if !t8LimiterHasKey(limiter, "new-key") {
		t.Errorf("新 key 应存在")
	}

	// 逐出后计数语义保持：同一 key 上限 2 → 第 3 次拒绝。
	if !limiter.Request("count-key", 2, 60) || !limiter.Request("count-key", 2, 60) {
		t.Fatalf("上限 2 的前两次请求应放行")
	}
	if limiter.Request("count-key", 2, 60) {
		t.Errorf("上限 2 的第 3 次请求应被拒绝")
	}
}

// TestInMemoryRateLimiterClose 覆盖 Close 幂等且未初始化时也安全（R2 §7）。
func TestInMemoryRateLimiterClose(t *testing.T) {
	// 未初始化的实例 Close 不应 panic。
	uninitialized := &InMemoryRateLimiter{}
	uninitialized.Close()
	uninitialized.Close()

	limiter := &InMemoryRateLimiter{}
	limiter.Init(time.Hour)
	limiter.Close()
	limiter.Close() // 幂等
	if size := limiter.Size(); size != 0 {
		t.Errorf("未记录请求时 Size 应为 0，实际 %d", size)
	}
}

// TestInMemoryRateLimiterCloseStopsGoroutines 覆盖 Close 能停止后台清理 goroutine，
// 避免反复创建实例时 goroutine 累积（R2 §7）。
func TestInMemoryRateLimiterCloseStopsGoroutines(t *testing.T) {
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	for i := 0; i < 50; i++ {
		limiter := &InMemoryRateLimiter{}
		limiter.Init(time.Hour)
		limiter.Close()
	}

	time.Sleep(300 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+8 {
		t.Errorf("Close 后清理 goroutine 未退出：before=%d after=%d", before, after)
	}
}
