package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// 注册表单令牌相关错误
var (
	ErrFormTokenDisabled = errors.New("registration form token is disabled")
	ErrFormTokenInvalid  = errors.New("registration form token is invalid")
	ErrFormTokenExpired  = errors.New("registration form token has expired")
	ErrFormTokenReused   = errors.New("registration form token has been used")
)

const (
	// formTokenVersion 令牌载荷版本号
	formTokenVersion = "v1"
	// formTokenStoreMaxEntries 单进程未过期 nonce 上限，防止未认证接口被刷爆内存
	formTokenStoreMaxEntries = 10000
	// formTokenDefaultExpiresIn 配置异常时的兜底有效期（秒）
	formTokenDefaultExpiresIn = 1800
	// formTokenRedisKeyPrefix Redis 中的 nonce 键前缀
	formTokenRedisKeyPrefix = "registration_form_token:"
	// formTokenRedisTTLBuffer Redis nonce 键在令牌有效期外额外保留的时间
	formTokenRedisTTLBuffer = 60 * time.Second
)

// formTokenRecord 已签发令牌的 nonce 记录，用于一次性（重放）校验
type formTokenRecord struct {
	used      bool
	expiresAt time.Time
}

var (
	formTokenStoreMu sync.Mutex
	formTokenStore   = make(map[string]*formTokenRecord)
)

// registrationTokenNow 允许测试替换时钟
var registrationTokenNow = time.Now

// RegistrationFormTokenEnabled 表单令牌是否启用（总开关关闭时视为关闭）
func RegistrationFormTokenEnabled() bool {
	setting := operation_setting.GetRegistrationSetting()
	return setting.Enabled && setting.FormTokenEnabled
}

// useRedisFormTokenStore 多实例部署时使用 Redis 做 nonce 一次性判定
func useRedisFormTokenStore() bool {
	return common.RedisEnabled && common.RDB != nil
}

func formTokenExpiresInSeconds(setting *operation_setting.RegistrationSetting) int {
	expiresIn := setting.FormTokenMaxMinutes * 60
	if expiresIn <= 0 {
		expiresIn = formTokenDefaultExpiresIn
	}
	return expiresIn
}

func newFormTokenNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// pruneFormTokenStoreLocked 清理已过期记录，调用方需持有 formTokenStoreMu
func pruneFormTokenStoreLocked(now time.Time) {
	for nonce, record := range formTokenStore {
		if !now.Before(record.expiresAt) {
			delete(formTokenStore, nonce)
		}
	}
}

// evictFormTokenEntriesLocked 在内存 nonce 表达到上限时逐出最旧记录，
// 保证内存有硬上限且不会因表满而拒绝签发（参照 common/verification.go 的逐出策略）。
// 调用方需持有 formTokenStoreMu。
func evictFormTokenEntriesLocked(now time.Time) {
	if len(formTokenStore) < formTokenStoreMaxEntries {
		return
	}

	pruneFormTokenStoreLocked(now)
	if len(formTokenStore) < formTokenStoreMaxEntries {
		return
	}

	// 仍然满：按过期时间（≈签发时间）从早到晚逐出，至少腾出 1 个位置。
	type entry struct {
		nonce     string
		expiresAt time.Time
	}
	entries := make([]entry, 0, len(formTokenStore))
	for nonce, record := range formTokenStore {
		entries = append(entries, entry{nonce: nonce, expiresAt: record.expiresAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].expiresAt.Before(entries[j].expiresAt)
	})

	excess := len(formTokenStore) - formTokenStoreMaxEntries + 1
	for i := 0; i < excess && i < len(entries); i++ {
		delete(formTokenStore, entries[i].nonce)
	}
	common.SysLog(fmt.Sprintf(
		"registration form token store exceeded %d entries; evicted %d oldest entries",
		formTokenStoreMaxEntries,
		excess,
	))
}

// IssueRegistrationFormToken 签发一次性注册表单令牌
//
// 返回值：token 为待提交的表单令牌，minSeconds 为最短填写时间（秒），
// expiresIn 为最长有效期（秒）。表单令牌关闭时返回 ErrFormTokenDisabled。
// 签名使用 common.GenerateHMAC（基于 CRYPTO_SECRET），多实例部署需共享该密钥；
// Redis 启用时 nonce 一次性判定在 Redis 中完成。
func IssueRegistrationFormToken() (token string, minSeconds, expiresIn int, err error) {
	setting := operation_setting.GetRegistrationSetting()
	if !setting.Enabled || !setting.FormTokenEnabled {
		return "", 0, 0, ErrFormTokenDisabled
	}

	minSeconds = setting.FormTokenMinSeconds
	if minSeconds < 0 {
		minSeconds = 0
	}
	expiresIn = formTokenExpiresInSeconds(setting)

	nonce, err := newFormTokenNonce()
	if err != nil {
		return "", 0, 0, err
	}

	now := registrationTokenNow()
	payload := strings.Join([]string{
		formTokenVersion,
		strconv.FormatInt(now.Unix(), 10),
		nonce,
	}, ":")

	// Redis 模式下 nonce 在消费时用 SETNX 原子写入，这里无需预存；
	// 内存模式下预存记录，供消费时判定一次性与未知 nonce。表满时逐出最旧条目，
	// 不拒绝签发（避免未认证请求刷满 nonce 表导致注册整体不可用）。
	if !useRedisFormTokenStore() {
		expiresAt := now.Add(time.Duration(expiresIn)*time.Second + time.Minute)
		formTokenStoreMu.Lock()
		evictFormTokenEntriesLocked(now)
		formTokenStore[nonce] = &formTokenRecord{expiresAt: expiresAt}
		formTokenStoreMu.Unlock()
	}

	return payload + ":" + common.GenerateHMAC(payload), minSeconds, expiresIn, nil
}

// ConsumeRegistrationFormToken 校验并消费一次性注册表单令牌
//
// 校验顺序：开关 → 常量时间签名 → 最短填写时间 → 有效期 → nonce 一次性。
// 表单令牌关闭时返回 ErrFormTokenDisabled；格式/签名/时间不合法返回
// ErrFormTokenInvalid；超过有效期返回 ErrFormTokenExpired；已消费返回
// ErrFormTokenReused。Redis 启用时用 SETNX 原子判定首次消费；Redis 故障时
// 记录日志并 fail-open（与登录锁定策略一致）。
func ConsumeRegistrationFormToken(token string) error {
	setting := operation_setting.GetRegistrationSetting()
	if !setting.Enabled || !setting.FormTokenEnabled {
		return ErrFormTokenDisabled
	}

	token = strings.TrimSpace(token)
	parts := strings.Split(token, ":")
	if len(parts) != 4 || parts[0] != formTokenVersion {
		return ErrFormTokenInvalid
	}

	issuedAtUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return ErrFormTokenInvalid
	}
	nonce := parts[2]
	if nonce == "" {
		return ErrFormTokenInvalid
	}

	payload := strings.Join(parts[:3], ":")
	expectedSignature := common.GenerateHMAC(payload)
	if subtle.ConstantTimeCompare([]byte(expectedSignature), []byte(parts[3])) != 1 {
		return ErrFormTokenInvalid
	}

	now := registrationTokenNow()
	if issuedAtUnix > now.Unix() {
		// 时钟回拨或伪造时间戳
		return ErrFormTokenInvalid
	}

	minSeconds := setting.FormTokenMinSeconds
	if minSeconds < 0 {
		minSeconds = 0
	}
	elapsed := now.Sub(time.Unix(issuedAtUnix, 0))
	if elapsed < time.Duration(minSeconds)*time.Second {
		return ErrFormTokenInvalid
	}

	// 多留 1 秒余量，避免边界秒误差导致合法令牌被判过期
	expiresIn := formTokenExpiresInSeconds(setting)
	if elapsed > time.Duration(expiresIn+1)*time.Second {
		return ErrFormTokenExpired
	}

	if useRedisFormTokenStore() {
		ttl := time.Duration(expiresIn)*time.Second + formTokenRedisTTLBuffer
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		firstUse, redisErr := common.RDB.SetNX(
			ctx,
			formTokenRedisKeyPrefix+nonce,
			"1",
			ttl,
		).Result()
		if redisErr != nil {
			// Redis 故障时 fail-open，避免表单令牌成为注册单点故障
			common.SysLog("registration form token: redis SETNX failed, fail-open: " + redisErr.Error())
			return nil
		}
		if !firstUse {
			return ErrFormTokenReused
		}
		return nil
	}

	formTokenStoreMu.Lock()
	defer formTokenStoreMu.Unlock()

	record, ok := formTokenStore[nonce]
	if !ok {
		return ErrFormTokenInvalid
	}
	if record.used {
		return ErrFormTokenReused
	}
	record.used = true

	return nil
}
