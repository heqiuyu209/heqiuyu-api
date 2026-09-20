package model

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"sync"

	"gorm.io/gorm"

	"github.com/heqiuyu/heqiuyu-api/common"
)

// channelEncryptionKeyEnv 可用环境变量覆盖渠道加密密钥（便于跨环境备份/迁移），
// 值为 base64 编码的 32 字节密钥，例如：$env:CHANNEL_ENCRYPTION_KEY="<base64>"
const channelEncryptionKeyEnv = "CHANNEL_ENCRYPTION_KEY"

// channelEncryptionKeyOption 渠道加密密钥在 Options 表的持久化 Key。
// 含 Secret 关键词的 Option 不会通过 GetOptions 回显给前端。
const channelEncryptionKeyOption = "ChannelEncryptionKey"

// GetChannelEncryptionKey 获取渠道 Key 加密密钥（固定 32 字节，AES-256）。
// 优先级：环境变量 CHANNEL_ENCRYPTION_KEY(base64) > Options 表持久化密钥 > 自动生成并持久化。
// 密钥在进程内缓存（sync.Once），首次加载在启动阶段完成（main.go 迁移前预热），
// 后续 BeforeSave/AfterFind hook 内只读缓存、不发起 DB 查询，避免事务内嵌套查询。
func GetChannelEncryptionKey() ([]byte, error) {
	channelEncryptionKeyOnce.Do(func() {
		channelEncryptionKeyCache, channelEncryptionKeyErr = loadChannelEncryptionKey()
	})
	return channelEncryptionKeyCache, channelEncryptionKeyErr
}

var (
	channelEncryptionKeyCache []byte
	channelEncryptionKeyErr   error
	channelEncryptionKeyOnce  sync.Once
)

func loadChannelEncryptionKey() ([]byte, error) {
	if v := os.Getenv(channelEncryptionKeyEnv); v != "" {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("invalid %s: must be base64 encoding of 32 bytes", channelEncryptionKeyEnv)
		}
		return key, nil
	}
	option := Option{}
	if err := DB.First(&option, "key = ?", channelEncryptionKeyOption).Error; err == nil && option.Value != "" {
		if key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(option.Value)); err == nil && len(key) == 32 {
			return key, nil
		}
		// Options 中已有密钥但无效：禁止静默覆盖，否则历史密文将全部不可解。
		// 应让运维介入：配置 CHANNEL_ENCRYPTION_KEY 环境变量，或确认密钥丢失后清空该 Option 与存量密文。
		return nil, fmt.Errorf("invalid persisted %s in Options (must be base64 encoding of 32 bytes); set %s env or clean up the option and existing ciphertexts manually", channelEncryptionKeyOption, channelEncryptionKeyEnv)
	}
	// 首次启动：自动生成并持久化，保证重启后密文可解
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate channel encryption key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	if err := UpdateOption(channelEncryptionKeyOption, encoded); err != nil {
		return nil, fmt.Errorf("failed to persist channel encryption key: %w", err)
	}
	common.SysLog("channel encryption key generated and persisted to Options")
	return key, nil
}

// EncryptChannelKey 加密渠道 Key（明文 -> "enc:v1:" 密文）。
func EncryptChannelKey(plain string) (string, error) {
	key, err := GetChannelEncryptionKey()
	if err != nil {
		return "", err
	}
	return common.AESGCMEncrypt(key, plain)
}

// DecryptChannelKey 解密渠道 Key（"enc:v1:" 密文 -> 明文）。
func DecryptChannelKey(cipher string) (string, error) {
	key, err := GetChannelEncryptionKey()
	if err != nil {
		return "", err
	}
	return common.AESGCMDecrypt(key, cipher)
}

// MaskChannelKey 脱敏渠道 Key：支持多 Key 换行分隔，按行分别保留前 4 位与后 4 位，
// 中间以 **** 代替，保持换行结构，便于前端按 Key 对应状态查看/辨识。
func MaskChannelKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	lines := strings.Split(key, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			lines[i] = ""
			continue
		}
		if len(line) <= 8 {
			lines[i] = strings.Repeat("*", len(line))
			continue
		}
		lines[i] = line[:4] + "****" + line[len(line)-4:]
	}
	return strings.Join(lines, "\n")
}

// MigrateChannelKeysToEncrypted 将存量明文 Key 的渠道迁移为加密存储（幂等），返回迁移条数。
// 仅在启动时调用一次；已带 enc:v1: 前缀的渠道自动跳过。
func MigrateChannelKeysToEncrypted() (int, error) {
	key, err := GetChannelEncryptionKey()
	if err != nil {
		return 0, err
	}
	var rows []struct {
		Id  int64
		Key string
	}
	// 用原生 SQL 只读原始 key 列，避免 Find 触发 AfterFind 解密导致幂等误判
	if err := DB.Raw("SELECT id, `key` FROM channels").Scan(&rows).Error; err != nil {
		return 0, err
	}
	migrated := 0
	for _, ch := range rows {
		if ch.Key == "" || strings.HasPrefix(ch.Key, common.EncryptedPrefix) {
			continue
		}
		enc, err := common.AESGCMEncrypt(key, ch.Key)
		if err != nil {
			return migrated, fmt.Errorf("encrypt channel %d: %w", ch.Id, err)
		}
		if err := DB.Model(&Channel{}).Where("id = ?", ch.Id).Update("key", enc).Error; err != nil {
			return migrated, fmt.Errorf("update channel %d: %w", ch.Id, err)
		}
		migrated++
	}
	if migrated > 0 {
		common.SysLog(fmt.Sprintf("channel keys migrated to encrypted storage: %d channels", migrated))
	}
	return migrated, nil
}

// BeforeSave 在渠道创建/更新前将明文 Key 加密落库；已是密文或 Key 为空时跳过。
func (channel *Channel) BeforeSave(tx *gorm.DB) error {
	if channel.Key == "" || strings.HasPrefix(channel.Key, common.EncryptedPrefix) {
		return nil
	}
	enc, err := EncryptChannelKey(channel.Key)
	if err != nil {
		return fmt.Errorf("failed to encrypt channel key: %w", err)
	}
	channel.Key = enc
	return nil
}

// AfterFind 在查询后自动解密渠道 Key，保证上层调用（relay/计费/状态轮询）对密文无感知。
// 解密失败（密钥丢失/被篡改）时记录严重日志并保留密文原值，避免阻断整表查询。
func (channel *Channel) AfterFind(tx *gorm.DB) error {
	if channel.Key == "" || !strings.HasPrefix(channel.Key, common.EncryptedPrefix) {
		return nil
	}
	dec, err := DecryptChannelKey(channel.Key)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to decrypt channel key, channel_id=%d: %v", channel.Id, err))
		return nil
	}
	channel.Key = dec
	return nil
}
