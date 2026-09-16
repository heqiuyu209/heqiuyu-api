package common

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type verificationValue struct {
	code string
	time time.Time
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
)

var (
	verificationMutex sync.Mutex
	verificationMap   map[string]verificationValue
	// verificationMapMaxSize 是内存实现的**真实**条目上限。
	//
	// 旧实现把上限写成 10，但只在超限时清理"已过期"的条目：10 分钟窗口内没有任何
	// 条目会过期，于是 map 实际上是无界增长的（未认证请求即可持续写入）。现在改为
	// 硬上限 + 逐出最旧条目，并在启用 Redis 时把状态放到 Redis（带 TTL、可跨节点共享）。
	verificationMapMaxSize   = 100000
	VerificationValidMinutes = 10
)

// verificationRedisKey 对用户标识做散列，避免把邮箱等明文写进 Redis 键。
func verificationRedisKey(purpose string, key string) string {
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("verify:%s:%s", purpose, hex.EncodeToString(sum[:16]))
}

// constantTimeEqual 以常量时间比较两个验证码/令牌，避免通过响应时间逐字节推断。
func constantTimeEqual(a string, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	if length > len(code) {
		length = len(code)
	}
	return code[:length]
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) {
	if RedisEnabled {
		ctx := context.Background()
		if err := RDB.Set(ctx, verificationRedisKey(purpose, key), code,
			time.Duration(VerificationValidMinutes)*time.Minute).Err(); err == nil {
			return
		}
		// Redis 写入失败时退回内存实现，保证注册/重置流程不中断。
		SysLog("failed to store verification code in redis, falling back to in-memory store")
	}

	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap[purpose+key] = verificationValue{
		code: code,
		time: time.Now(),
	}
	evictVerificationEntriesLocked()
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	if RedisEnabled {
		ctx := context.Background()
		stored, err := RDB.Get(ctx, verificationRedisKey(purpose, key)).Result()
		if err == nil {
			return constantTimeEqual(stored, code)
		}
	}

	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	now := time.Now()
	if !okay || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 {
		return false
	}
	return constantTimeEqual(value.code, code)
}

func DeleteKey(key string, purpose string) {
	if RedisEnabled {
		if err := RDB.Del(context.Background(), verificationRedisKey(purpose, key)).Err(); err != nil {
			SysLog("failed to delete verification code in redis: " + err.Error())
		}
	}

	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// evictVerificationEntriesLocked 先清理过期条目，若仍超过上限则逐出最旧的条目。
// 调用方必须已持有 verificationMutex。
func evictVerificationEntriesLocked() {
	if len(verificationMap) <= verificationMapMaxSize {
		return
	}
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
	if len(verificationMap) <= verificationMapMaxSize {
		return
	}

	// 仍然超限：按写入时间逐出最旧的条目，确保内存占用有硬上限。
	type entry struct {
		key  string
		time time.Time
	}
	entries := make([]entry, 0, len(verificationMap))
	for key, value := range verificationMap {
		entries = append(entries, entry{key: key, time: value.time})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].time.Before(entries[j].time)
	})
	excess := len(verificationMap) - verificationMapMaxSize
	for i := 0; i < excess && i < len(entries); i++ {
		delete(verificationMap, entries[i].key)
	}
	SysLog(fmt.Sprintf("verification store exceeded %d entries; evicted %d oldest entries", verificationMapMaxSize, excess))
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
