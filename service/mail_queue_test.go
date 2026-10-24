package service

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

var errT8MailBoom = errors.New("t8 fake mail sender failure")

// t8MailQueueSetup 为每个用例准备确定性的异步邮件配置与干净队列状态。
func t8MailQueueSetup(t *testing.T) *operation_setting.RegistrationSetting {
	t.Helper()

	prevRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedisEnabled })

	setting := operation_setting.GetRegistrationSetting()
	saved := *setting
	t.Cleanup(func() { *setting = saved }) // 先注册 → 最后执行

	*setting = operation_setting.RegistrationSetting{
		Enabled:                   true,
		AsyncEmailEnabled:         true,
		AsyncEmailQueueSize:       256,
		AsyncEmailWorkers:         2,
		AsyncEmailMaxPerMinute:    60,
		AsyncEmailBreakerFailures: 5,
	}

	ResetMailQueueForTests()
	t.Cleanup(func() {
		// 先换成安全的假发送，避免在 worker 读取 sender 与清理之间出现竞态
		// 而落到 common.SendEmail（本测试必须保证不触碰真实 SMTP）。
		SetMailSenderForTest(func(subject, to, content string) error { return nil })
		time.Sleep(20 * time.Millisecond)
		ResetMailQueueForTests()
	})
	return setting
}

// TestEnqueueEmailSyncFallbackWhenAsyncDisabled 覆盖 async_email_enabled=false 的回滚路径：
// 同步调用发送函数并把错误原样返回给 controller（controller 据此删码并返回 503）。
func TestEnqueueEmailSyncFallbackWhenAsyncDisabled(t *testing.T) {
	setting := t8MailQueueSetup(t)
	setting.AsyncEmailEnabled = false

	var (
		mu   sync.Mutex
		seen []string
	)
	SetMailSenderForTest(func(subject, to, content string) error {
		mu.Lock()
		seen = append(seen, subject+"|"+to+"|"+content)
		mu.Unlock()
		return errT8MailBoom
	})

	err := EnqueueEmail("verification", "sync@example.com", "subject-sync", "content-sync")
	if !errors.Is(err, errT8MailBoom) {
		t.Fatalf("同步回退应原样返回发送错误，实际: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != "subject-sync|sync@example.com|content-sync" {
		t.Errorf("同步发送参数不符合预期: %v", seen)
	}
}

// TestMailQueueRateLimit 覆盖 R2 新语义：发信速率槽在 worker 真正 SMTP 发送前获取。
//
// 旧断言"第 3 次 EnqueueEmail 直接返回 ErrMailRateLimited（入队即占预算）"已过时：
// 现在入队只做背压/熔断快速检查，速率槽由 worker 在发送前 acquireSendSlot 获取；
// 达到上限时 worker 等待下一分钟窗口（最长 60s，超时丢弃）。
//
// 为避免长时间 sleep，本用例只断言可快速观测的部分：
//  1. 前 2 封真实发送后，第 3 封不得立即发送（worker 在等速率槽）；
//  2. 窗口预算已满时，新的入队请求快速失败返回 ErrMailRateLimited。
func TestMailQueueRateLimit(t *testing.T) {
	setting := t8MailQueueSetup(t)
	setting.AsyncEmailMaxPerMinute = 2
	setting.AsyncEmailWorkers = 1
	setting.AsyncEmailQueueSize = 8

	var (
		mu    sync.Mutex
		sends int
	)
	sent := make(chan struct{}, 8)
	SetMailSenderForTest(func(subject, to, content string) error {
		mu.Lock()
		sends++
		mu.Unlock()
		select {
		case sent <- struct{}{}:
		default:
		}
		return nil
	})

	for i := 0; i < 3; i++ {
		if err := EnqueueEmail("verification", "rate"+strconv.Itoa(i)+"@example.com", "s", "c"); err != nil {
			t.Fatalf("第 %d 封应成功入队（入队只做背压/熔断检查），实际: %v", i+1, err)
		}
	}

	// 等待前 2 封真实发送完毕（每封发送前已占用一个速率槽）。
	for i := 0; i < 2; i++ {
		select {
		case <-sent:
		case <-time.After(5 * time.Second):
			mu.Lock()
			got := sends
			mu.Unlock()
			t.Fatalf("等待前 2 封发送超时，实际发送 %d 封", got)
		}
	}

	// 第 3 封：worker 已取得任务但拿不到速率槽，应处于等待状态（不立即发送）。
	time.Sleep(400 * time.Millisecond)
	mu.Lock()
	got := sends
	mu.Unlock()
	if got > 2 {
		t.Errorf("达到 async_email_max_per_minute=2 后第 3 封不应立即发送，实际已发送 %d 封", got)
	}

	// 窗口预算已满：新的入队请求快速失败（不再等到发送阶段才拒绝）。
	if err := EnqueueEmail("verification", "rate-fastfail@example.com", "s", "c"); !errors.Is(err, ErrMailRateLimited) {
		t.Fatalf("窗口预算已满时新的入队请求应返回 ErrMailRateLimited，实际: %v", err)
	}

	// 速率窗口计数应恰为上限（第 3 封未取得槽，不得增加计数）。
	mailGate.mu.Lock()
	windowCount := mailGate.windowCount
	mailGate.mu.Unlock()
	if windowCount != 2 {
		t.Errorf("速率窗口计数应保持 2（第 3 封在等待槽），实际 %d", windowCount)
	}
}

// TestEnqueueEmailSyncRespectsRateLimit 覆盖 R2 §6：async_email_enabled=false 的
// 同步回退路径也必须受全站发信速率约束。
func TestEnqueueEmailSyncRespectsRateLimit(t *testing.T) {
	setting := t8MailQueueSetup(t)
	setting.AsyncEmailEnabled = false
	setting.AsyncEmailMaxPerMinute = 2
	SetMailSenderForTest(func(subject, to, content string) error { return nil })

	for i := 0; i < 2; i++ {
		if err := EnqueueEmail("verification", "sync-rate"+strconv.Itoa(i)+"@example.com", "s", "c"); err != nil {
			t.Fatalf("同步路径第 %d 封应发送成功，实际: %v", i+1, err)
		}
	}
	if err := EnqueueEmail("verification", "sync-rate-3@example.com", "s", "c"); !errors.Is(err, ErrMailRateLimited) {
		t.Fatalf("同步路径达到 async_email_max_per_minute=2 后应返回 ErrMailRateLimited，实际: %v", err)
	}
}

// TestEnqueueEmailSyncRespectsBreaker 覆盖 R2 §6：同步发送失败同样计入连续失败/熔断。
func TestEnqueueEmailSyncRespectsBreaker(t *testing.T) {
	setting := t8MailQueueSetup(t)
	setting.AsyncEmailEnabled = false
	setting.AsyncEmailBreakerFailures = 1
	SetMailSenderForTest(func(subject, to, content string) error { return errT8MailBoom })

	// 第一次同步发送：底层错误原样返回，同时计入连续失败并触发熔断。
	if err := EnqueueEmail("verification", "sync-breaker-1@example.com", "s", "c"); !errors.Is(err, errT8MailBoom) {
		t.Fatalf("同步路径首次发送失败应返回底层错误，实际: %v", err)
	}
	// 第二次：熔断已打开，快速失败。
	if err := EnqueueEmail("verification", "sync-breaker-2@example.com", "s", "c"); !errors.Is(err, ErrMailBreakerOpen) {
		t.Fatalf("同步路径连续失败达到阈值后应返回 ErrMailBreakerOpen，实际: %v", err)
	}
}

// TestEnqueueEmailSyncAtomicRateLimitConcurrent 覆盖 R2 收口（task-25）：
// async=false 的同步回退路径通过原子 tryAcquireSendSlot 占用全站发信速率槽，
// 并发调用时成功数不得超出 async_email_max_per_minute，其余应返回 ErrMailRateLimited。
func TestEnqueueEmailSyncAtomicRateLimitConcurrent(t *testing.T) {
	setting := t8MailQueueSetup(t)
	setting.AsyncEmailEnabled = false
	setting.AsyncEmailMaxPerMinute = 2
	SetMailSenderForTest(func(subject, to, content string) error { return nil })

	const workers = 12
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // 尽量让 12 个 goroutine 同时竞争速率槽
			errs[idx] = EnqueueEmail(
				"verification",
				"sync-atomic-"+strconv.Itoa(idx)+"@example.com",
				"s",
				"c",
			)
		}(i)
	}
	close(start)
	wg.Wait()

	success, rateLimited := 0, 0
	for idx, err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrMailRateLimited):
			rateLimited++
		default:
			t.Errorf("并发同步发送返回非预期错误（index %d）: %v", idx, err)
		}
	}
	if success != 2 {
		t.Errorf("并发下同步发送成功数应恰好等于 async_email_max_per_minute=2，实际 %d", success)
	}
	if rateLimited != workers-2 {
		t.Errorf("其余 %d 次应返回 ErrMailRateLimited，实际 %d", workers-2, rateLimited)
	}
	// 同步路径不经过 worker 等待丢弃分支，丢弃计数应保持 0。
	if dropped := MailQueueDroppedCount(); dropped != 0 {
		t.Errorf("同步路径不应产生 worker 丢弃计数，实际 %d", dropped)
	}
	// ResetMailQueueForTests 应同时归零速率窗口与丢弃计数。
	ResetMailQueueForTests()
	if dropped := MailQueueDroppedCount(); dropped != 0 {
		t.Errorf("ResetMailQueueForTests 后 MailQueueDroppedCount 应为 0，实际 %d", dropped)
	}
}

// TestMailQueueDroppedCountHookAvailable 覆盖 T5 新增丢弃计数钩子的读取与 Reset 归零语义；
// worker 等待 60s 超时丢弃分支不做长测（见 acceptance §8.3-3）。
func TestMailQueueDroppedCountHookAvailable(t *testing.T) {
	_ = t8MailQueueSetup(t)

	if dropped := MailQueueDroppedCount(); dropped != 0 {
		t.Errorf("初始 MailQueueDroppedCount 应为 0，实际 %d", dropped)
	}
	// 同包内模拟累计（仅验证钩子读取与 Reset 归零，不触发 60s 等待分支）。
	mailDroppedTotal.Add(3)
	if dropped := MailQueueDroppedCount(); dropped != 3 {
		t.Errorf("MailQueueDroppedCount 应读取累计值 3，实际 %d", dropped)
	}
	ResetMailQueueForTests()
	if dropped := MailQueueDroppedCount(); dropped != 0 {
		t.Errorf("ResetMailQueueForTests 后 MailQueueDroppedCount 应归零，实际 %d", dropped)
	}
}

// TestMailQueueFull 覆盖有界队列容量（队列长度/worker 数为重启生效参数）。
func TestMailQueueFull(t *testing.T) {
	setting := t8MailQueueSetup(t)
	setting.AsyncEmailQueueSize = 1
	setting.AsyncEmailWorkers = 1

	block := make(chan struct{})
	started := make(chan struct{}, 1)
	SetMailSenderForTest(func(subject, to, content string) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-block
		return nil
	})
	t.Cleanup(func() { close(block) })

	// 第 1 封：worker 取走并在 sender 中阻塞（队列空）。
	if err := EnqueueEmail("verification", "full1@example.com", "s1", "c1"); err != nil {
		t.Fatalf("第 1 封邮件应成功入队，实际: %v", err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatalf("worker 未在 5 秒内开始发送第 1 封邮件")
	}

	// 第 2 封：worker 忙，进入容量为 1 的队列 → 成功。
	if err := EnqueueEmail("verification", "full2@example.com", "s2", "c2"); err != nil {
		t.Fatalf("第 2 封邮件应成功入队（队列容量 1），实际: %v", err)
	}
	// 第 3 封：队列满 → ErrMailQueueFull。
	if err := EnqueueEmail("verification", "full3@example.com", "s3", "c3"); !errors.Is(err, ErrMailQueueFull) {
		t.Fatalf("队列已满时应返回 ErrMailQueueFull，实际: %v", err)
	}
}

// TestMailBreakerOpensAfterConsecutiveFailures 覆盖连续失败熔断与失败重试：
// 单封邮件失败后间隔 2 秒重试 1 次，仍失败才计入熔断；重置后恢复。
func TestMailBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	setting := t8MailQueueSetup(t)
	setting.AsyncEmailBreakerFailures = 1

	var (
		mu    sync.Mutex
		calls int
	)
	called := make(chan struct{}, 8)
	SetMailSenderForTest(func(subject, to, content string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		select {
		case called <- struct{}{}:
		default:
		}
		return errT8MailBoom
	})

	if err := EnqueueEmail("verification", "breaker@example.com", "s", "c"); err != nil {
		t.Fatalf("首封邮件应成功入队，实际: %v", err)
	}

	// 1 次首次发送 + 1 次重试 → 至少 2 次调用；重试间隔 2 秒。
	for i := 0; i < 2; i++ {
		select {
		case <-called:
		case <-time.After(6 * time.Second):
			mu.Lock()
			got := calls
			mu.Unlock()
			t.Fatalf("等待发送调用超时：期望至少 2 次（首次+重试），实际 %d 次", got)
		}
	}

	// 给 recordSendResult 一点点时间落熔断状态（它在 deliver 返回后立即执行）。
	var breakerErr error
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		breakerErr = EnqueueEmail("verification", "breaker2@example.com", "s2", "c2")
		if errors.Is(breakerErr, ErrMailBreakerOpen) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !errors.Is(breakerErr, ErrMailBreakerOpen) {
		t.Fatalf("连续失败达到 async_email_breaker_failures=1 后应熔断（ErrMailBreakerOpen），实际: %v", breakerErr)
	}

	// ResetMailQueueForTests 必须清除熔断状态，恢复发送。
	ResetMailQueueForTests()
	SetMailSenderForTest(func(subject, to, content string) error { return nil })
	if err := EnqueueEmail("verification", "breaker3@example.com", "s3", "c3"); err != nil {
		t.Fatalf("ResetMailQueueForTests 后应恢复发送，实际: %v", err)
	}
}
