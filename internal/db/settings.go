package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const settingCipherPrefix = "enc:v1:"

// NormalizeSettingScope 规范化系统配置作用域名称（空值或 "global" 归为 "global"）。
func NormalizeSettingScope(scope string) string {
	s := strings.ToLower(strings.TrimSpace(scope))
	if s == "" || s == "global" {
		return "global"
	}
	return s
}

// SystemSettingID 生成系统配置表唯一主键（"<scope>:<key>"）。
func SystemSettingID(scope, key string) string {
	return NormalizeSettingScope(scope) + ":" + strings.TrimSpace(key)
}

func deriveSettingAEAD(secret string) (cipher.AEAD, error) {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		trimmed = "default-local-salt"
	}
	sum := sha256.Sum256([]byte("ai-web-free/system-settings/" + trimmed))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// EncryptSettingSecret 使用 AES-256-GCM 对敏感配置值进行加密，返回带版本前缀的密文。
func EncryptSettingSecret(plain, secret string) (string, error) {
	if plain == "" {
		return "", nil
	}
	aead, err := deriveSettingAEAD(secret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(plain), nil)
	return settingCipherPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptSettingSecret 解密敏感配置密文；支持向后兼容未加密的明文以及空密钥回退尝试。
func DecryptSettingSecret(cipherText, secret string) (string, error) {
	if cipherText == "" {
		return "", nil
	}
	if !strings.HasPrefix(cipherText, settingCipherPrefix) {
		return cipherText, nil
	}
	rawB64 := strings.TrimPrefix(cipherText, settingCipherPrefix)
	raw, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		return "", fmt.Errorf("invalid setting cipher encoding: %w", err)
	}
	tryDecrypt := func(sec string) (string, error) {
		aead, err := deriveSettingAEAD(sec)
		if err != nil {
			return "", err
		}
		ns := aead.NonceSize()
		if len(raw) < ns {
			return "", fmt.Errorf("cipher payload too short")
		}
		out, err := aead.Open(nil, raw[:ns], raw[ns:], nil)
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
	if plain, err := tryDecrypt(secret); err == nil {
		return plain, nil
	}
	if strings.TrimSpace(secret) != "" {
		if plain, err := tryDecrypt(""); err == nil {
			return plain, nil
		}
	}
	return "", fmt.Errorf("failed to decrypt system setting secret")
}

// MaskSecretValue 对敏感配置值做掩码脱敏预览（保留前后端少量字符以便辨识）。
func MaskSecretValue(val string) string {
	if val == "" {
		return ""
	}
	runes := []rune(val)
	n := len(runes)
	if n <= 4 {
		return strings.Repeat("•", n)
	}
	if n <= 12 {
		return string(runes[:2]) + strings.Repeat("•", n-4) + string(runes[n-2:])
	}
	return string(runes[:6]) + "••••••" + string(runes[n-4:])
}

// DecodeSettingValue 解析单条数据库配置记录的真实值（非敏感项读 Value，敏感项解密 ValueCipher）。
func DecodeSettingValue(rec SystemSettingRecord, secret string) (string, error) {
	if !rec.IsSecret {
		return rec.Value, nil
	}
	if rec.ValueCipher != "" {
		return DecryptSettingSecret(rec.ValueCipher, secret)
	}
	return rec.Value, nil
}

// SystemSettingUpsertItem 描述待写入数据库的单个系统配置项。
type SystemSettingUpsertItem struct {
	Key      string
	Category string
	Value    string
	IsSecret bool
}

// LoadSystemSettings 读取数据库中全部系统配置记录。
func LoadSystemSettings(database *gorm.DB) ([]SystemSettingRecord, error) {
	if database == nil {
		return nil, nil
	}
	var records []SystemSettingRecord
	if err := database.Order("scope asc, category asc, key asc").Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// LoadSystemSettingsByScope 读取指定 Scope 下的全部系统配置，返回 key -> record 映射。
func LoadSystemSettingsByScope(database *gorm.DB, scope string) (map[string]SystemSettingRecord, error) {
	out := make(map[string]SystemSettingRecord)
	if database == nil {
		return out, nil
	}
	normScope := NormalizeSettingScope(scope)
	var records []SystemSettingRecord
	if err := database.Where("scope = ?", normScope).Find(&records).Error; err != nil {
		return nil, err
	}
	for _, r := range records {
		out[r.Key] = r
	}
	return out, nil
}

// UpsertSystemSettings 批量写入或更新指定 Scope 下的系统配置（敏感字段自动 AES-GCM 加密）。
func UpsertSystemSettings(database *gorm.DB, scope string, items []SystemSettingUpsertItem, secret, updatedBy string) error {
	if database == nil || len(items) == 0 {
		return nil
	}
	normScope := NormalizeSettingScope(scope)
	if strings.TrimSpace(updatedBy) == "" {
		updatedBy = "admin"
	}
	now := time.Now().UTC()
	records := make([]SystemSettingRecord, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		rec := SystemSettingRecord{
			ID:        SystemSettingID(normScope, key),
			Scope:     normScope,
			Key:       key,
			Category:  strings.TrimSpace(item.Category),
			IsSecret:  item.IsSecret,
			UpdatedBy: updatedBy,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if item.IsSecret {
			enc, err := EncryptSettingSecret(item.Value, secret)
			if err != nil {
				return err
			}
			rec.Value = ""
			rec.ValueCipher = enc
		} else {
			rec.Value = item.Value
			rec.ValueCipher = ""
		}
		records = append(records, rec)
	}
	if len(records) == 0 {
		return nil
	}
	return database.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"scope", "key", "category", "value", "value_cipher", "is_secret", "updated_by", "updated_at",
		}),
	}).CreateInBatches(records, 100).Error
}

// DeleteSystemSetting 删除指定 Scope 下的某个配置覆盖记录。
func DeleteSystemSetting(database *gorm.DB, scope, key string) error {
	if database == nil || strings.TrimSpace(key) == "" {
		return nil
	}
	id := SystemSettingID(scope, key)
	return database.Where("id = ?", id).Delete(&SystemSettingRecord{}).Error
}

// RevealSystemSetting 解密并返回指定 Scope（若当前 Profile 未覆盖则按需回退到 global）下的配置明文。
func RevealSystemSetting(database *gorm.DB, scope, key, secret string) (string, string, bool, error) {
	if database == nil || strings.TrimSpace(key) == "" {
		return "", "", false, nil
	}
	normScope := NormalizeSettingScope(scope)
	var rec SystemSettingRecord
	err := database.Where("id = ?", SystemSettingID(normScope, key)).Take(&rec).Error
	if err == nil {
		plain, decErr := DecodeSettingValue(rec, secret)
		return plain, normScope, true, decErr
	}
	if normScope != "global" {
		err = database.Where("id = ?", SystemSettingID("global", key)).Take(&rec).Error
		if err == nil {
			plain, decErr := DecodeSettingValue(rec, secret)
			return plain, "global", true, decErr
		}
	}
	return "", "", false, nil
}

// ListSystemSettingScopes 返回数据库中出现过的所有非 global Profile Scope 列表。
func ListSystemSettingScopes(database *gorm.DB) []string {
	if database == nil {
		return nil
	}
	var scopes []string
	_ = database.Model(&SystemSettingRecord{}).Distinct("scope").Pluck("scope", &scopes).Error
	return scopes
}
