package model

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/heqiuyu/heqiuyu-api/common"
)

// ensureTestOptionMap 保证 updateOptionMap 写 map 时不 panic（OptionMap 未初始化时先建空 map）
func ensureTestOptionMap() {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMapRWMutex.Unlock()
}

func TestAESGCMRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plain := "sk-abcdef1234567890"
	enc, err := common.AESGCMEncrypt(key, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(enc, common.EncryptedPrefix) {
		t.Fatalf("missing prefix: %s", enc)
	}
	dec, err := common.AESGCMDecrypt(key, enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if dec != plain {
		t.Fatalf("roundtrip mismatch: %q != %q", dec, plain)
	}
	bad := make([]byte, 32)
	copy(bad, key)
	bad[0] ^= 0xff
	if _, err := common.AESGCMDecrypt(bad, enc); err == nil {
		t.Fatal("expected decrypt failure with wrong key")
	}
	if _, err := common.AESGCMDecrypt(key, "sk-plain"); err == nil {
		t.Fatal("expected error for non-prefixed input")
	}
}

func TestChannelEncryptionKeyStable(t *testing.T) {
	ensureTestOptionMap()
	if err := DB.AutoMigrate(&Option{}); err != nil {
		t.Fatalf("migrate option: %v", err)
	}
	key, err := GetChannelEncryptionKey()
	if err != nil {
		t.Fatalf("get key: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}
	key2, err := GetChannelEncryptionKey()
	if err != nil {
		t.Fatalf("get key again: %v", err)
	}
	for i := range key {
		if key[i] != key2[i] {
			t.Fatal("channel encryption key not stable across calls")
		}
	}
}

func TestChannelSaveEncryptsAndFindDecrypts(t *testing.T) {
	ensureTestOptionMap()
	if err := DB.AutoMigrate(&Channel{}, &Option{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 预热加密密钥缓存，避免 Create 事务内嵌套查询
	if _, err := GetChannelEncryptionKey(); err != nil {
		t.Fatalf("preheat key: %v", err)
	}
	ch := &Channel{
		Name: "test-enc",
		Type: 1,
		Key:  "sk-plain-secret-123456",
	}
	if err := DB.Create(ch).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var raw Channel
	if err := DB.Raw("SELECT * FROM channels WHERE id = ?", ch.Id).Scan(&raw).Error; err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if !strings.HasPrefix(raw.Key, common.EncryptedPrefix) {
		t.Fatalf("stored key is not encrypted: %q", raw.Key)
	}
	got, err := GetChannelById(ch.Id, true)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Key != "sk-plain-secret-123456" {
		t.Fatalf("decrypted key mismatch: %q", got.Key)
	}
	// 更新后再次查询应拿到新明文（Updates 经 BeforeSave 重新加密落库）
	got.Key = "sk-updated-888888"
	if err := DB.Model(got).Updates(got).Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, err := GetChannelById(ch.Id, true)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got2.Key != "sk-updated-888888" {
		t.Fatalf("updated key mismatch: %q", got2.Key)
	}
}

func TestMigrateChannelKeysToEncrypted(t *testing.T) {
	ensureTestOptionMap()
	if err := DB.AutoMigrate(&Channel{}, &Option{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 预热加密密钥缓存
	if _, err := GetChannelEncryptionKey(); err != nil {
		t.Fatalf("preheat key: %v", err)
	}
	plain1 := Channel{Name: "legacy-1", Type: 1, Key: "sk-legacy-1-plain"}
	plain2 := Channel{Name: "legacy-2", Type: 1, Key: "sk-legacy-2-plain"}
	// 用 SkipHooks 直接写入明文，模拟修复前落库的存量数据
	if err := DB.Session(&gorm.Session{SkipHooks: true}).Create(&plain1).Error; err != nil {
		t.Fatalf("create plain1: %v", err)
	}
	if err := DB.Session(&gorm.Session{SkipHooks: true}).Create(&plain2).Error; err != nil {
		t.Fatalf("create plain2: %v", err)
	}
	before, err := GetChannelById(plain1.Id, true)
	if err != nil {
		t.Fatalf("get before migrate: %v", err)
	}
	if before.Key != "sk-legacy-1-plain" {
		t.Fatalf("expected plaintext before migrate, got %q", before.Key)
	}
	n, err := MigrateChannelKeysToEncrypted()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 2 {
		t.Fatalf("migrated = %d, want 2", n)
	}
	got, err := GetChannelById(plain1.Id, true)
	if err != nil {
		t.Fatalf("get after migrate: %v", err)
	}
	if got.Key != "sk-legacy-1-plain" {
		t.Fatalf("decrypted key mismatch after migrate: %q", got.Key)
	}
	var raw Channel
	if err := DB.Raw("SELECT * FROM channels WHERE id = ?", plain1.Id).Scan(&raw).Error; err != nil {
		t.Fatalf("raw after migrate: %v", err)
	}
	if !strings.HasPrefix(raw.Key, common.EncryptedPrefix) {
		t.Fatalf("key still plaintext after migrate: %q", raw.Key)
	}
	// 幂等：再次迁移应 0 条
	n2, err := MigrateChannelKeysToEncrypted()
	if err != nil {
		t.Fatalf("migrate again: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second migrate = %d, want 0", n2)
	}
}

func TestMaskChannelKey(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"abc":                  "***",
		"abcdefgh":             "********",
		"sk-abcdef1234567890":  "sk-a****7890",
		"  sk-x1234567890abcd ": "sk-x****abcd",
	}
	for in, want := range cases {
		if got := MaskChannelKey(in); got != want {
			t.Fatalf("MaskChannelKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskChannelKeyMultiLine(t *testing.T) {
	// 多 Key 渠道以换行分隔：必须按行分别脱敏并保留换行，前端才能按 Key 辨识状态
	in := "sk-line-one-123456\nsk-line-two-654321"
	want := "sk-l****3456\nsk-l****4321"
	if got := MaskChannelKey(in); got != want {
		t.Fatalf("MaskChannelKey(multiline) = %q, want %q", got, want)
	}
	// 短 key 行整体打星，空行保留
	in2 := "abcd\n\nsk-abcdef1234567890"
	want2 := "****\n\nsk-a****7890"
	if got := MaskChannelKey(in2); got != want2 {
		t.Fatalf("MaskChannelKey(multiline2) = %q, want %q", got, want2)
	}
}
