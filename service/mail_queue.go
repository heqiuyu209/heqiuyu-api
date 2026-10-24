package service

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// 邮件发送错误。controller 据此决定返回 429 还是 503。
var (
	ErrMailQueueFull   = errors.New("mail queue is full")
	ErrMailBreakerOpen = errors.New("mail circuit breaker is open")
	ErrMailRateLimited = errors.New("mail send rate limit exceeded")
)

// mailDroppedTotal 累计因等待发信速率槽超过 mailSendSlotMaxWait 而丢弃的邮件数。
var mailDroppedTotal atomic.Int64

// MailQueueDroppedCount 返回累计丢弃的邮件数（监控/测试用）。
func MailQueueDroppedCount() int64 {
	return mailDroppedTotal.Load()
}

const (
	mailQueueLogPrefix = "[mail-queue]"

	mailQueueDefaultSize       = 256
	mailQueueDefaultWorkers    = 2
	mailQueueDefaultPerMinute  = 60
	mailQueueDefaultBreakerNum = 5
	mailBreakerOpenDuration    = 60 * time.Second

	// mailRetryInterval / mailRetryMaxAttempts 定义单封邮件的失败重试策略：
	// 首次失败后间隔 2 秒重试 1 次（计划 §5.5），重试仍失败才计入熔断计数。
	mailRetryInterval    = 2 * time.Second
	mailRetryMaxAttempts = 1

	// mailQueueRateWindow 是全站发信速率窗口：每分钟最多 N 封。
	mailQueueRateWindow = time.Minute

	// mailSendSlotMaxWait 是 worker 为等待发信速率槽的最长时间；超时丢弃并记日志，
	// 避免 SMTP 故障/预算耗尽时 worker 无限堆积。
	mailSendSlotMaxWait = 60 * time.Second
)

// mailSendGate 是全站发信速率与熔断状态，与队列实例解耦：
// 异步队列（worker 发送失败/成功）与 async_email_enabled=false 的同步回退
// 共用同一份预算与熔断状态，保证关闭异步队列不会绕过限速/熔断。
type mailSendGate struct {
	mu                  sync.Mutex
	consecutiveFailures int
	breakerUntil        time.Time
	windowStart         time.Time
	windowCount         int
}

var mailGate = &mailSendGate{}

// allowSend 检查熔断状态与全站发信速率（每分钟）；不计数，计数见 markSent。
func (g *mailSendGate) allowSend(now time.Time) error {
	maxPerMinute := operation_setting.GetRegistrationSetting().AsyncEmailMaxPerMinute
	if maxPerMinute <= 0 {
		maxPerMinute = mailQueueDefaultPerMinute
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Before(g.breakerUntil) {
		return ErrMailBreakerOpen
	}
	if g.windowStart.IsZero() || now.Sub(g.windowStart) >= mailQueueRateWindow {
		g.windowStart = now
		g.windowCount = 0
	}
	if g.windowCount >= maxPerMinute {
		return ErrMailRateLimited
	}
	return nil
}

// tryAcquireSendSlot 原子地占用一个全站发信速率槽。
//
// 达到 async_email_max_per_minute 时返回 ErrMailRateLimited（不等待）。
// 同步回退路径在发送前直接调用；worker 的阻塞等待基于同一函数循环实现。
func (g *mailSendGate) tryAcquireSendSlot() error {
	maxPerMinute := operation_setting.GetRegistrationSetting().AsyncEmailMaxPerMinute
	if maxPerMinute <= 0 {
		maxPerMinute = mailQueueDefaultPerMinute
	}
	now := time.Now()

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.windowStart.IsZero() || now.Sub(g.windowStart) >= mailQueueRateWindow {
		g.windowStart = now
		g.windowCount = 0
	}
	if g.windowCount >= maxPerMinute {
		return ErrMailRateLimited
	}
	g.windowCount++
	return nil
}

// recordResult 更新"连续失败→熔断"状态，并记录日志。
func (g *mailSendGate) recordResult(kind string, err error) {
	breakerFailures := operation_setting.GetRegistrationSetting().AsyncEmailBreakerFailures
	if breakerFailures <= 0 {
		breakerFailures = mailQueueDefaultBreakerNum
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if err != nil {
		g.consecutiveFailures++
		common.SysLog(fmt.Sprintf("%s send %s email failed (consecutive failures: %d): %v",
			mailQueueLogPrefix, kind, g.consecutiveFailures, err))
		if g.consecutiveFailures >= breakerFailures {
			g.breakerUntil = time.Now().Add(mailBreakerOpenDuration)
			g.consecutiveFailures = 0
			common.SysLog(fmt.Sprintf("%s circuit breaker opened for %s after %d consecutive failures",
				mailQueueLogPrefix, mailBreakerOpenDuration, breakerFailures))
		}
		return
	}
	g.consecutiveFailures = 0
}

// acquireSendSlot 在真正 SMTP 发送前占用一个全站发信速率槽。
//
// 达到 async_email_max_per_minute 时等待到下一分钟窗口；最长等待
// mailSendSlotMaxWait（60s），超时返回 ErrMailRateLimited，由 worker 丢弃并记日志。
func (g *mailSendGate) acquireSendSlot(stop <-chan struct{}) error {
	deadline := time.Now().Add(mailSendSlotMaxWait)
	for {
		err := g.tryAcquireSendSlot()
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrMailRateLimited) {
			return err
		}

		now := time.Now()
		g.mu.Lock()
		wait := g.windowStart.Add(mailQueueRateWindow).Sub(now)
		g.mu.Unlock()

		if wait <= 0 {
			wait = 10 * time.Millisecond
		}
		if now.Add(wait).After(deadline) {
			return ErrMailRateLimited
		}
		select {
		case <-stop:
			return errors.New("mail queue is stopped")
		case <-time.After(wait):
		}
	}
}

// reset 清空速率窗口与熔断状态（测试用）。
func (g *mailSendGate) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.consecutiveFailures = 0
	g.breakerUntil = time.Time{}
	g.windowStart = time.Time{}
	g.windowCount = 0
}

// mailTask 是队列中的待发送邮件。
type mailTask struct {
	kind    string
	to      string
	subject string
	content string
}

// mailQueueState 是有界异步邮件队列的运行时状态。
//
// 队列长度与 worker 数在初始化时读取配置（计划 2.2 标注为"重启生效"）；
// 全站发信速率与熔断状态放在包级 mailSendGate 中（与队列解耦、支持热调）。
type mailQueueState struct {
	queue chan mailTask
	stop  chan struct{}
}

var (
	mailQueueMu sync.Mutex
	mailQueue   *mailQueueState

	mailSenderMu       sync.RWMutex
	mailSenderOverride func(subject, to, content string) error
)

// currentMailSender 返回当前使用的发送函数（测试可替换）。
func currentMailSender() func(subject, to, content string) error {
	mailSenderMu.RLock()
	defer mailSenderMu.RUnlock()
	if mailSenderOverride != nil {
		return mailSenderOverride
	}
	return common.SendEmail
}

// newMailQueueState 按当前配置创建队列并启动 worker。
func newMailQueueState(setting *operation_setting.RegistrationSetting) *mailQueueState {
	queueSize := setting.AsyncEmailQueueSize
	if queueSize <= 0 {
		queueSize = mailQueueDefaultSize
	}
	workers := setting.AsyncEmailWorkers
	if workers <= 0 {
		workers = mailQueueDefaultWorkers
	}
	if workers > queueSize {
		workers = queueSize
	}

	state := &mailQueueState{
		queue: make(chan mailTask, queueSize),
		stop:  make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		go state.worker()
	}
	return state
}

// currentMailQueue 懒初始化队列（配置在首次初始化时读取）。
func currentMailQueue() *mailQueueState {
	mailQueueMu.Lock()
	defer mailQueueMu.Unlock()
	if mailQueue == nil {
		mailQueue = newMailQueueState(operation_setting.GetRegistrationSetting())
	}
	return mailQueue
}

// stopLocked 停止队列的 worker（调用方需持有 mailQueueMu）。
func (q *mailQueueState) stopWorkers() {
	close(q.stop)
}

// worker 是单个发信 goroutine。
func (q *mailQueueState) worker() {
	for {
		select {
		case <-q.stop:
			return
		case task := <-q.queue:
			// 真实发送前先取全站发信速率槽：达到每分钟上限时等待到下一窗口，
			// 等待超过上限则丢弃并记日志（避免 worker 长期被预算阻塞）。
			if slotErr := mailGate.acquireSendSlot(q.stop); slotErr != nil {
				total := mailDroppedTotal.Add(1)
				common.SysLog(fmt.Sprintf("%s drop %s email (total dropped: %d): %v", mailQueueLogPrefix, task.kind, total, slotErr))
				continue
			}
			err := q.deliver(task)
			mailGate.recordResult(task.kind, err)
		}
	}
}

// deliver 发送一封邮件，失败后间隔 2 秒重试 1 次（只重试 1 次，避免堵塞队列）。
func (q *mailQueueState) deliver(task mailTask) error {
	err := currentMailSender()(task.subject, task.to, task.content)
	if err == nil {
		return nil
	}
	for attempt := 1; attempt <= mailRetryMaxAttempts; attempt++ {
		select {
		case <-q.stop:
			// 队列正在关闭：不再重试，按失败上报。
			return err
		case <-time.After(mailRetryInterval):
		}
		common.SysLog(fmt.Sprintf("%s retrying %s email after failure (attempt %d/%d): %v",
			mailQueueLogPrefix, task.kind, attempt, mailRetryMaxAttempts, err))
		err = currentMailSender()(task.subject, task.to, task.content)
		if err == nil {
			return nil
		}
	}
	return err
}

// enqueue 把邮件放入有界队列。
//
// 入队只做"熔断 + 当前速率余量"的快速检查（保证背压/快速失败），
// 真正的发信速率槽由 worker 在 SMTP 发送前获取（见 acquireSendSlot）。
func (q *mailQueueState) enqueue(task mailTask) error {
	now := time.Now()
	if err := mailGate.allowSend(now); err != nil {
		return err
	}
	select {
	case q.queue <- task:
		return nil
	default:
		return ErrMailQueueFull
	}
}

// sendEmailSync 是 async_email_enabled=false 时的同步回退路径。
//
// 必须与异步路径共用 mailSendGate：受全站发信速率与熔断约束，同步失败同样
// 计入连续失败/熔断，返回 ErrMailBreakerOpen / ErrMailRateLimited 语义一致。
// 速率槽用 tryAcquireSendSlot 原子占用（并发下不会超出预算），发送后只
// recordResult，不再额外 markSent（避免双计）。
func sendEmailSync(task mailTask) error {
	if err := mailGate.allowSend(time.Now()); err != nil {
		return err
	}
	if err := mailGate.tryAcquireSendSlot(); err != nil {
		return err
	}
	err := currentMailSender()(task.subject, task.to, task.content)
	mailGate.recordResult(task.kind, err)
	return err
}

// EnqueueEmail 发送一封邮件（默认异步）。
//
// kind 仅用于日志（如 verification/password-reset）。异步发信关闭时退化为
// 同步发送，但同样经过全站限速/熔断（见 sendEmailSync）。
//
// 返回 ErrMailQueueFull / ErrMailBreakerOpen 时 controller 应删除刚保存的验证码
// 并返回 user.email_send_unavailable；返回 ErrMailRateLimited 时应返回 429。
func EnqueueEmail(kind, to, subject, content string) error {
	setting := operation_setting.GetRegistrationSetting()
	task := mailTask{kind: kind, to: to, subject: subject, content: content}
	if !setting.AsyncEmailEnabled {
		return sendEmailSync(task)
	}
	return currentMailQueue().enqueue(task)
}

// SetMailSenderForTest 替换发信函数（仅测试使用）。传 nil 恢复默认实现。
func SetMailSenderForTest(fn func(subject, to, content string) error) {
	mailSenderMu.Lock()
	mailSenderOverride = fn
	mailSenderMu.Unlock()
}

// ResetMailQueueForTests 重置队列、熔断与速率状态（仅测试使用）。
func ResetMailQueueForTests() {
	mailQueueMu.Lock()
	if mailQueue != nil {
		mailQueue.stopWorkers()
	}
	mailQueue = nil
	mailQueueMu.Unlock()

	mailSenderMu.Lock()
	mailSenderOverride = nil
	mailSenderMu.Unlock()

	mailGate.reset()
	mailDroppedTotal.Store(0)
}
