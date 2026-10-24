package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// registrationGuardLogPrefix 用于运维文档中的监控日志关键字。
const registrationGuardLogPrefix = "[registration-guard]"

// RegistrationDecision 是注册/发码限流检查结果。
//
// RetryAfterSeconds 仅在 Allowed=false 时有意义，供 HTTP Retry-After 响应头使用。
type RegistrationDecision struct {
	Allowed           bool
	Reason            string
	RetryAfterSeconds int
}

// Reason 取值（仅供日志与测试断言，不直接暴露给前端）。
const (
	RegistrationDenyAttemptWindow       = "attempt_window"
	RegistrationDenyIPHourlySuccess     = "ip_success_hour"
	RegistrationDenyIPDailySuccess      = "ip_success_day"
	RegistrationDenyGlobalHourlySuccess = "global_success_hour"
	RegistrationDenyEmailCooldown       = "email_cooldown"
	RegistrationDenyEmailDaily          = "email_day"
	RegistrationDenyEmailIPHourly       = "email_ip_hour"
	RegistrationDenyEmailGlobalHourly   = "email_global_hour"
)

const (
	// guardMemoryCleanupInterval 是内存限流器的清理周期。必须不小于任何窗口
	// （最长窗口为 24 小时），否则清理协程会误删仍在窗口内的计数。
	guardMemoryCleanupInterval = 24 * time.Hour

	guardDefaultAttemptWindowSeconds = 600
	guardDefaultVerifyWindowSeconds  = 600
	guardDefaultVerifyMaxAttempts    = 5
	guardDefaultEmailPurpose         = "v"
)

// ---------- 决策辅助 ----------

func registrationAllowedDecision() RegistrationDecision {
	return RegistrationDecision{Allowed: true}
}

func registrationDeniedDecision(reason string, retryAfterSeconds int) RegistrationDecision {
	if retryAfterSeconds < 1 {
		retryAfterSeconds = 1
	}
	return RegistrationDecision{
		Allowed:           false,
		Reason:            reason,
		RetryAfterSeconds: retryAfterSeconds,
	}
}

// ---------- 配置辅助（每次读取，保证后台热生效） ----------

func registrationGuardSetting() *operation_setting.RegistrationSetting {
	return operation_setting.GetRegistrationSetting()
}

func positiveSeconds(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func untilWindowBoundary(now time.Time, periodSeconds int64) int {
	if periodSeconds <= 0 {
		return 1
	}
	remaining := periodSeconds - now.Unix()%periodSeconds
	if remaining <= 0 {
		return int(periodSeconds)
	}
	return int(remaining)
}

func durationSecondsCeil(d time.Duration) int {
	if d <= 0 {
		return 1
	}
	return int(math.Ceil(d.Seconds()))
}

// normalizeEmailForGuard 与发信/注册流程使用相同的规范化规则。
func normalizeEmailForGuard(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// emailGuardHash 对邮箱做散列，避免把明文邮箱写进 Redis 键/内存键。
func emailGuardHash(email string) string {
	sum := sha256.Sum256([]byte(email))
	return hex.EncodeToString(sum[:16])
}

// registrationIPAllowlisted 判断 IP 是否命中永久放行名单（逗号分隔 IP/CIDR）。
func registrationIPAllowlisted(ip string, allowlist string) bool {
	allowlist = strings.TrimSpace(allowlist)
	ip = strings.TrimSpace(ip)
	if allowlist == "" || ip == "" {
		return false
	}
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	for _, entry := range strings.Split(allowlist, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, cidr, err := net.ParseCIDR(entry); err == nil && cidr.Contains(parsedIP) {
				return true
			}
			continue
		}
		if allowedIP := net.ParseIP(entry); allowedIP != nil && allowedIP.Equal(parsedIP) {
			return true
		}
	}
	return false
}

// ---------- Redis 实现 ----------

func guardRedisEnabled() bool {
	return common.RedisEnabled && common.RDB != nil
}

func guardLogRedisFailure(operation string, err error) {
	common.SysLog(fmt.Sprintf("%s redis %s failed (fail-open): %v", registrationGuardLogPrefix, operation, err))
}

// guardRedisIncr 自增计数并保证键一定带有 TTL。
//
// 注意：common/limiter 的 Lua 脚本里 EXPIRE 被注释掉了，直接复用会产生永不
// 过期的键。这里改为 INCR + EXPIRE 双命令，并在 EXPIRE 失败时删除键兜底，
// 确保不会留下永不过期的限流键。
func guardRedisIncr(key string, ttl time.Duration) (int64, error) {
	ctx := context.Background()
	count, err := common.RDB.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	if err := common.RDB.Expire(ctx, key, ttl).Err(); err != nil {
		guardLogRedisFailure("expire "+key, err)
		// EXPIRE 失败时删除键，宁可丢失计数也不留下永不过期的键。
		if delErr := common.RDB.Del(ctx, key).Err(); delErr != nil {
			guardLogRedisFailure("delete "+key, delErr)
		}
	}
	return count, nil
}

// guardRedisGet 读取计数及其剩余 TTL（键不存在时返回 0/0/nil）。
func guardRedisGet(key string) (int64, time.Duration, error) {
	ctx := context.Background()
	count, err := common.RDB.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	ttl, ttlErr := common.RDB.TTL(ctx, key).Result()
	if ttlErr != nil || ttl < 0 {
		ttl = 0
	}
	return count, ttl, nil
}

// guardRedisCooldownPeek 检查冷却键是否仍在冷却中。
func guardRedisCooldownPeek(key string) (bool, time.Duration, error) {
	ctx := context.Background()
	ttl, err := common.RDB.TTL(ctx, key).Result()
	if err != nil {
		return false, 0, err
	}
	// -2：键不存在；-1：键存在但无过期时间（不应出现，按冷却处理并兜底）。
	if ttl == -2 {
		return true, 0, nil
	}
	if ttl < 0 {
		return false, 0, nil
	}
	return false, ttl, nil
}

// guardRedisTakeCooldown 尝试占用冷却键（SET NX EX）。
func guardRedisTakeCooldown(key string, ttl time.Duration) (bool, time.Duration, error) {
	ctx := context.Background()
	ok, err := common.RDB.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, 0, err
	}
	if ok {
		return true, 0, nil
	}
	remaining, ttlErr := common.RDB.TTL(ctx, key).Result()
	if ttlErr != nil || remaining < 0 {
		remaining = ttl
	}
	return false, remaining, nil
}

// ---------- 内存实现 ----------
//
// 内存实现复用 common.InMemoryRateLimiter（滑动窗口）。清理周期使用 24 小时，
// 覆盖最长的"每日"窗口；否则默认 20 分钟的清理周期会误删日计数。

var (
	guardMemoryMu      sync.Mutex
	guardMemoryLimiter *common.InMemoryRateLimiter
)

func guardMemory() *common.InMemoryRateLimiter {
	guardMemoryMu.Lock()
	defer guardMemoryMu.Unlock()
	if guardMemoryLimiter == nil {
		limiter := &common.InMemoryRateLimiter{}
		limiter.Init(guardMemoryCleanupInterval)
		guardMemoryLimiter = limiter
	}
	return guardMemoryLimiter
}

// guardMemoryRequest 记录一次请求，返回是否仍在限额内。
func guardMemoryRequest(key string, max int, windowSeconds int) bool {
	if max <= 0 || windowSeconds <= 0 {
		return true
	}
	return guardMemory().Request(key, max, int64(windowSeconds))
}

// guardMemoryPeek 只检查是否已达上限，不记录本次请求。
func guardMemoryPeek(key string, max int, windowSeconds int) bool {
	if max <= 0 || windowSeconds <= 0 {
		return true
	}
	return guardMemory().Peek(key, max, int64(windowSeconds))
}

// ---------- 对外接口 ----------

// CheckRegistrationAttempt 检查一次注册请求是否允许。
//
// 检查项：per-IP 窗口内尝试数（含失败）、per-IP 每小时/每天成功数、全站每小时成功数。
// 成功计数由 handler 在注册成功后调用 RecordRegistrationSuccess 回写。
func CheckRegistrationAttempt(ip string) RegistrationDecision {
	setting := registrationGuardSetting()
	if !setting.Enabled {
		return registrationAllowedDecision()
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return registrationAllowedDecision()
	}
	if registrationIPAllowlisted(ip, setting.IpAllowlist) {
		return registrationAllowedDecision()
	}
	now := time.Now()

	if guardRedisEnabled() {
		// 1) 窗口内尝试数：每个注册请求都要计数（含失败请求）。
		if setting.IpMaxAttemptsPerWindow > 0 {
			windowSeconds := positiveSeconds(setting.IpAttemptWindowSeconds, guardDefaultAttemptWindowSeconds)
			bucket := now.Unix() / int64(windowSeconds)
			key := fmt.Sprintf("registration:guard:attempt:%s:%d", ip, bucket)
			// TTL 到窗口结束即可，键过期后计数自然重置。
			ttl := time.Duration(untilWindowBoundary(now, int64(windowSeconds))) * time.Second
			count, err := guardRedisIncr(key, ttl)
			if err != nil {
				guardLogRedisFailure("registration attempt counter", err)
			} else if count > int64(setting.IpMaxAttemptsPerWindow) {
				return registrationDeniedDecision(RegistrationDenyAttemptWindow, untilWindowBoundary(now, int64(windowSeconds)))
			}
		}
		// 2) per-IP 每小时成功上限。
		if setting.IpMaxSuccessPerHour > 0 {
			key := fmt.Sprintf("registration:guard:success:hour:%s:%d", ip, now.Unix()/3600)
			count, _, err := guardRedisGet(key)
			if err != nil {
				guardLogRedisFailure("ip hourly success counter", err)
			} else if count >= int64(setting.IpMaxSuccessPerHour) {
				return registrationDeniedDecision(RegistrationDenyIPHourlySuccess, untilWindowBoundary(now, 3600))
			}
		}
		// 3) per-IP 每天成功上限。
		if setting.IpMaxSuccessPerDay > 0 {
			key := fmt.Sprintf("registration:guard:success:day:%s:%d", ip, now.Unix()/86400)
			count, _, err := guardRedisGet(key)
			if err != nil {
				guardLogRedisFailure("ip daily success counter", err)
			} else if count >= int64(setting.IpMaxSuccessPerDay) {
				return registrationDeniedDecision(RegistrationDenyIPDailySuccess, untilWindowBoundary(now, 86400))
			}
		}
		// 4) 全站每小时成功上限（0=关闭）。
		if setting.GlobalMaxSuccessPerHour > 0 {
			key := fmt.Sprintf("registration:guard:success:global:hour:%d", now.Unix()/3600)
			count, _, err := guardRedisGet(key)
			if err != nil {
				guardLogRedisFailure("global hourly success counter", err)
			} else if count >= int64(setting.GlobalMaxSuccessPerHour) {
				return registrationDeniedDecision(RegistrationDenyGlobalHourlySuccess, untilWindowBoundary(now, 3600))
			}
		}
		return registrationAllowedDecision()
	}

	// 内存实现（滑动窗口）。
	if setting.IpMaxAttemptsPerWindow > 0 {
		windowSeconds := positiveSeconds(setting.IpAttemptWindowSeconds, guardDefaultAttemptWindowSeconds)
		if !guardMemoryRequest("registration:attempt:"+ip, setting.IpMaxAttemptsPerWindow, windowSeconds) {
			return registrationDeniedDecision(RegistrationDenyAttemptWindow, windowSeconds)
		}
	}
	if setting.IpMaxSuccessPerHour > 0 {
		if !guardMemoryPeek("registration:success:hour:"+ip, setting.IpMaxSuccessPerHour, 3600) {
			return registrationDeniedDecision(RegistrationDenyIPHourlySuccess, 3600)
		}
	}
	if setting.IpMaxSuccessPerDay > 0 {
		if !guardMemoryPeek("registration:success:day:"+ip, setting.IpMaxSuccessPerDay, 86400) {
			return registrationDeniedDecision(RegistrationDenyIPDailySuccess, 86400)
		}
	}
	if setting.GlobalMaxSuccessPerHour > 0 {
		if !guardMemoryPeek("registration:success:global:hour", setting.GlobalMaxSuccessPerHour, 3600) {
			return registrationDeniedDecision(RegistrationDenyGlobalHourlySuccess, 3600)
		}
	}
	return registrationAllowedDecision()
}

// RecordRegistrationSuccess 记录一次成功注册（由注册 handler 在入库成功后回写）。
func RecordRegistrationSuccess(ip string) {
	setting := registrationGuardSetting()
	if !setting.Enabled {
		return
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	if registrationIPAllowlisted(ip, setting.IpAllowlist) {
		return
	}
	now := time.Now()

	if guardRedisEnabled() {
		if setting.IpMaxSuccessPerHour > 0 {
			key := fmt.Sprintf("registration:guard:success:hour:%s:%d", ip, now.Unix()/3600)
			if _, err := guardRedisIncr(key, 2*time.Hour); err != nil {
				guardLogRedisFailure("record ip hourly success", err)
			}
		}
		if setting.IpMaxSuccessPerDay > 0 {
			key := fmt.Sprintf("registration:guard:success:day:%s:%d", ip, now.Unix()/86400)
			if _, err := guardRedisIncr(key, 48*time.Hour); err != nil {
				guardLogRedisFailure("record ip daily success", err)
			}
		}
		if setting.GlobalMaxSuccessPerHour > 0 {
			key := fmt.Sprintf("registration:guard:success:global:hour:%d", now.Unix()/3600)
			if _, err := guardRedisIncr(key, 2*time.Hour); err != nil {
				guardLogRedisFailure("record global hourly success", err)
			}
		}
		return
	}

	if setting.IpMaxSuccessPerHour > 0 {
		guardMemoryRequest("registration:success:hour:"+ip, setting.IpMaxSuccessPerHour, 3600)
	}
	if setting.IpMaxSuccessPerDay > 0 {
		guardMemoryRequest("registration:success:day:"+ip, setting.IpMaxSuccessPerDay, 86400)
	}
	if setting.GlobalMaxSuccessPerHour > 0 {
		guardMemoryRequest("registration:success:global:hour", setting.GlobalMaxSuccessPerHour, 3600)
	}
}

// CheckEmailSend 检查一次验证码/重置邮件发送是否允许。
//
// 检查项：同邮箱冷却、同邮箱每日上限、单 IP 每小时发码上限、全站每小时发码预算。
// 全部带 TTL/窗口，Redis 故障时 fail-open 并记录日志。
func CheckEmailSend(ip, email, purpose string) RegistrationDecision {
	setting := registrationGuardSetting()
	if !setting.Enabled {
		return registrationAllowedDecision()
	}
	ip = strings.TrimSpace(ip)
	if registrationIPAllowlisted(ip, setting.IpAllowlist) {
		return registrationAllowedDecision()
	}
	email = normalizeEmailForGuard(email)
	if email == "" {
		return registrationAllowedDecision()
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		purpose = guardDefaultEmailPurpose
	}
	now := time.Now()
	emailHash := emailGuardHash(email)

	cooldownKey := fmt.Sprintf("registration:guard:email:cooldown:%s:%s", purpose, emailHash)
	dayKey := fmt.Sprintf("registration:guard:email:day:%s:%s:%d", purpose, emailHash, now.Unix()/86400)
	ipHourKey := fmt.Sprintf("registration:guard:email:ip:hour:%s:%d", ip, now.Unix()/3600)
	globalHourKey := fmt.Sprintf("registration:guard:email:global:hour:%d", now.Unix()/3600)

	if guardRedisEnabled() {
		// 先只检查不改计数，避免被拒绝的请求消耗配额。
		if setting.EmailCooldownSeconds > 0 {
			allowed, retryAfter, err := guardRedisCooldownPeek(cooldownKey)
			if err != nil {
				guardLogRedisFailure("email cooldown peek", err)
			} else if !allowed {
				return registrationDeniedDecision(RegistrationDenyEmailCooldown, durationSecondsCeil(retryAfter))
			}
		}
		if setting.EmailMaxPerDay > 0 {
			count, _, err := guardRedisGet(dayKey)
			if err != nil {
				guardLogRedisFailure("email daily counter", err)
			} else if count >= int64(setting.EmailMaxPerDay) {
				return registrationDeniedDecision(RegistrationDenyEmailDaily, untilWindowBoundary(now, 86400))
			}
		}
		if setting.EmailIpMaxPerHour > 0 {
			count, _, err := guardRedisGet(ipHourKey)
			if err != nil {
				guardLogRedisFailure("email ip hourly counter", err)
			} else if count >= int64(setting.EmailIpMaxPerHour) {
				return registrationDeniedDecision(RegistrationDenyEmailIPHourly, untilWindowBoundary(now, 3600))
			}
		}
		if setting.EmailGlobalMaxPerHour > 0 {
			count, _, err := guardRedisGet(globalHourKey)
			if err != nil {
				guardLogRedisFailure("email global hourly counter", err)
			} else if count >= int64(setting.EmailGlobalMaxPerHour) {
				return registrationDeniedDecision(RegistrationDenyEmailGlobalHourly, untilWindowBoundary(now, 3600))
			}
		}

		// 检查通过后提交：先占冷却键（原子），再累加计数。
		if setting.EmailCooldownSeconds > 0 {
			cooldown := time.Duration(setting.EmailCooldownSeconds) * time.Second
			allowed, retryAfter, err := guardRedisTakeCooldown(cooldownKey, cooldown)
			if err != nil {
				guardLogRedisFailure("email cooldown take", err)
			} else if !allowed {
				return registrationDeniedDecision(RegistrationDenyEmailCooldown, durationSecondsCeil(retryAfter))
			}
		}
		if setting.EmailMaxPerDay > 0 {
			if _, err := guardRedisIncr(dayKey, 48*time.Hour); err != nil {
				guardLogRedisFailure("email daily counter incr", err)
			}
		}
		if setting.EmailIpMaxPerHour > 0 {
			if _, err := guardRedisIncr(ipHourKey, 2*time.Hour); err != nil {
				guardLogRedisFailure("email ip hourly counter incr", err)
			}
		}
		if setting.EmailGlobalMaxPerHour > 0 {
			if _, err := guardRedisIncr(globalHourKey, 2*time.Hour); err != nil {
				guardLogRedisFailure("email global hourly counter incr", err)
			}
		}
		return registrationAllowedDecision()
	}

	// 内存实现（滑动窗口）。
	// 冷却必须"检查+计数"原子完成：Request 在限流器内部持锁判断并计数。若先 Peek
	// 再 Request，并发请求可同时通过冷却检查，导致同邮箱在冷却期内发出多封邮件。
	if setting.EmailCooldownSeconds > 0 {
		if !guardMemoryRequest(cooldownKey, 1, setting.EmailCooldownSeconds) {
			return registrationDeniedDecision(RegistrationDenyEmailCooldown, setting.EmailCooldownSeconds)
		}
	}
	if setting.EmailMaxPerDay > 0 {
		if !guardMemoryPeek("registration:email:day:"+purpose+":"+emailHash, setting.EmailMaxPerDay, 86400) {
			return registrationDeniedDecision(RegistrationDenyEmailDaily, 86400)
		}
	}
	if setting.EmailIpMaxPerHour > 0 {
		if !guardMemoryPeek("registration:email:ip:hour:"+ip, setting.EmailIpMaxPerHour, 3600) {
			return registrationDeniedDecision(RegistrationDenyEmailIPHourly, 3600)
		}
	}
	if setting.EmailGlobalMaxPerHour > 0 {
		if !guardMemoryPeek("registration:email:global:hour", setting.EmailGlobalMaxPerHour, 3600) {
			return registrationDeniedDecision(RegistrationDenyEmailGlobalHourly, 3600)
		}
	}
	// 检查通过后提交计数（冷却已在上面原子完成检查+计数，不再重复提交）。
	if setting.EmailMaxPerDay > 0 {
		guardMemoryRequest("registration:email:day:"+purpose+":"+emailHash, setting.EmailMaxPerDay, 86400)
	}
	if setting.EmailIpMaxPerHour > 0 {
		guardMemoryRequest("registration:email:ip:hour:"+ip, setting.EmailIpMaxPerHour, 3600)
	}
	if setting.EmailGlobalMaxPerHour > 0 {
		guardMemoryRequest("registration:email:global:hour", setting.EmailGlobalMaxPerHour, 3600)
	}
	return registrationAllowedDecision()
}

// RegisterVerificationFailure 记录一次验证码校验失败。
//
// 返回 locked=true 表示该邮箱在窗口内的错误次数已达上限；retryAfterSeconds 为
// 建议的重试等待秒数（达到上限后返回窗口剩余时间/完整窗口）。
func RegisterVerificationFailure(email, purpose string) (locked bool, retryAfterSeconds int) {
	setting := registrationGuardSetting()
	if !setting.Enabled {
		return false, 0
	}
	email = normalizeEmailForGuard(email)
	if email == "" {
		return false, 0
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		purpose = guardDefaultEmailPurpose
	}
	maxAttempts := setting.VerificationMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = guardDefaultVerifyMaxAttempts
	}
	windowSeconds := positiveSeconds(setting.VerificationAttemptWindowSeconds, guardDefaultVerifyWindowSeconds)
	emailHash := emailGuardHash(email)
	memoryKey := "registration:verify:fail:" + purpose + ":" + emailHash

	if guardRedisEnabled() {
		redisKey := "registration:guard:verify:fail:" + purpose + ":" + emailHash
		count, err := guardRedisIncr(redisKey, time.Duration(windowSeconds)*time.Second)
		if err != nil {
			guardLogRedisFailure("verification failure counter", err)
			return false, 0
		}
		if count >= int64(maxAttempts) {
			_, ttl, ttlErr := guardRedisGet(redisKey)
			if ttlErr != nil || ttl <= 0 {
				return true, windowSeconds
			}
			return true, durationSecondsCeil(ttl)
		}
		return false, 0
	}

	// maxAttempts+1 用于完整记录第 maxAttempts 次失败；随后用 Peek 判定锁定。
	guardMemoryRequest(memoryKey, maxAttempts+1, windowSeconds)
	if !guardMemoryPeek(memoryKey, maxAttempts, windowSeconds) {
		return true, windowSeconds
	}
	return false, 0
}

// ClearVerificationFailures 清除某邮箱的验证码错误计数（验证成功/注册成功后调用）。
func ClearVerificationFailures(email, purpose string) {
	email = normalizeEmailForGuard(email)
	if email == "" {
		return
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		purpose = guardDefaultEmailPurpose
	}
	emailHash := emailGuardHash(email)
	if guardRedisEnabled() {
		ctx := context.Background()
		redisKey := "registration:guard:verify:fail:" + purpose + ":" + emailHash
		if err := common.RDB.Del(ctx, redisKey).Err(); err != nil {
			guardLogRedisFailure("clear verification failures", err)
		}
	}
	guardMemory().Delete("registration:verify:fail:" + purpose + ":" + emailHash)
}

// ResetRegistrationGuardForTests 重置内存限流状态（仅测试使用）。
//
// 先 Close 旧的内存限流器（停止其后台清理 goroutine），再创建新实例，
// 避免测试反复重置时 goroutine 累积。
// Redis 计数不在重置范围内；测试通过 common.RedisEnabled=false 使用内存实现。
func ResetRegistrationGuardForTests() {
	guardMemoryMu.Lock()
	if guardMemoryLimiter != nil {
		guardMemoryLimiter.Close()
	}
	limiter := &common.InMemoryRateLimiter{}
	limiter.Init(guardMemoryCleanupInterval)
	guardMemoryLimiter = limiter
	guardMemoryMu.Unlock()
}
