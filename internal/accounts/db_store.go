package accounts

import (
	"strings"
	"time"

	"ai-web-free/internal/db"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func parseAccountType(s string) AccountType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "noauth":
		return TypeNoAuth
	case "puid":
		return TypePUID
	default:
		return TypeFree
	}
}

func parseAccountStatus(s string) AccountStatus {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pending":
		return StatusPending
	case "expired":
		return StatusExpired
	case "rate_limited":
		return StatusRateLimited
	case "disabled":
		return StatusDisabled
	case "banned":
		return StatusBanned
	case "standby":
		return StatusStandby
	default:
		return StatusActive
	}
}

// AccountToRecord 将内存 Account 转换为持久化 DB 记录
func AccountToRecord(a *Account) *db.PoolAccountRecord {
	if a == nil {
		return nil
	}
	id := strings.TrimSpace(a.ID)
	if id == "" {
		id = uuid.NewString()
		a.ID = id
	}

	rec := &db.PoolAccountRecord{
		ID:               id,
		Provider:         "chatgpt",
		Type:             a.Type.String(),
		Email:            strings.TrimSpace(a.Email),
		Password:         a.Password,
		TOTPSecret:       a.TOTPSecret,
		Token:            a.Token,
		RefreshToken:     a.RefreshToken,
		SessionToken:     a.SessionToken,
		PUID:             a.PUID,
		TeamUserID:       a.TeamUserID,
		ChatGPTAccountID: a.ChatGPTAccountID,
		PlanType:         a.PlanType,
		Proxy:            a.Proxy,
		Status:           a.Status.String(),
		LastError:        a.LastError,
		RenewState:       a.RenewState,
		RenewFailures:    a.RenewFailures,
		LastRenewError:   a.LastRenewError,
		TotalCalls:       a.TotalCalls,
		FailedCalls:      a.FailedCalls,
		CreatedAt:        a.CreatedAt,
		UpdatedAt:        time.Now(),
	}

	if !a.ExpiresAt.IsZero() {
		t := a.ExpiresAt
		rec.ExpiresAt = &t
	}
	if !a.CooldownUntil.IsZero() {
		t := a.CooldownUntil
		rec.CooldownUntil = &t
	}
	if !a.ImageCooldownUntil.IsZero() {
		t := a.ImageCooldownUntil
		rec.ImageCooldownUntil = &t
	}
	if !a.LastUsed.IsZero() {
		t := a.LastUsed
		rec.LastUsed = &t
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	return rec
}

// RecordToAccount 将 DB 记录恢复为内存 Account 对象，基于 seedKey 确定性绑定指纹画像
func RecordToAccount(r *db.PoolAccountRecord, profilePool []FingerprintProfile) *Account {
	if r == nil {
		return nil
	}
	acctType := parseAccountType(r.Type)
	acct := NewAccount(r.ID, acctType, r.Token)
	acct.Email = r.Email
	acct.Password = r.Password
	acct.TOTPSecret = r.TOTPSecret
	acct.RefreshToken = r.RefreshToken
	acct.SessionToken = r.SessionToken
	acct.PUID = r.PUID
	acct.TeamUserID = r.TeamUserID
	acct.ChatGPTAccountID = r.ChatGPTAccountID
	acct.PlanType = r.PlanType
	acct.Proxy = r.Proxy
	acct.Status = parseAccountStatus(r.Status)
	acct.LastError = r.LastError
	acct.RenewState = r.RenewState
	acct.RenewFailures = r.RenewFailures
	acct.LastRenewError = r.LastRenewError
	acct.TotalCalls = r.TotalCalls
	acct.FailedCalls = r.FailedCalls
	if !r.CreatedAt.IsZero() {
		acct.CreatedAt = r.CreatedAt
	}

	if r.ExpiresAt != nil {
		acct.ExpiresAt = *r.ExpiresAt
	}
	if r.CooldownUntil != nil {
		acct.CooldownUntil = *r.CooldownUntil
	}
	if r.ImageCooldownUntil != nil {
		acct.ImageCooldownUntil = *r.ImageCooldownUntil
	}
	if r.LastUsed != nil {
		acct.LastUsed = *r.LastUsed
	}

	seedKey := acct.Email
	if seedKey == "" {
		seedKey = acct.ChatGPTAccountID
	}
	if seedKey == "" {
		seedKey = acct.Token
	}
	acct.Fingerprint = deterministicProfile(seedKey, profilePool)

	if acct.Token != "" && (acct.ChatGPTAccountID == "" || acct.Email == "") {
		acct.SyncJWTMetadata()
	}

	return acct
}

// SaveAccountToDB 将单个账号幂等保存/更新至 SQLite，支持凭据合并防丢失
func SaveAccountToDB(database *gorm.DB, a *Account) error {
	if database == nil || a == nil || a.IsTemporary {
		return nil
	}
	rec := AccountToRecord(a)
	if rec == nil {
		return nil
	}

	// 查找是否已存在同名/同 ID 账号以保证主键稳定并合并已有密钥凭据
	var existing db.PoolAccountRecord
	query := database.Where("id = ?", rec.ID)
	if rec.Email != "" && rec.ChatGPTAccountID != "" {
		query = database.Where("id = ? OR (email != '' AND LOWER(email) = ?) OR (chatgpt_account_id != '' AND chatgpt_account_id = ?)",
			rec.ID, strings.ToLower(rec.Email), rec.ChatGPTAccountID)
	} else if rec.Email != "" {
		query = database.Where("id = ? OR (email != '' AND LOWER(email) = ?)", rec.ID, strings.ToLower(rec.Email))
	} else if rec.ChatGPTAccountID != "" {
		query = database.Where("id = ? OR (chatgpt_account_id != '' AND chatgpt_account_id = ?)", rec.ID, rec.ChatGPTAccountID)
	}

	if err := query.First(&existing).Error; err == nil && existing.ID != "" {
		rec.ID = existing.ID
		a.ID = existing.ID
		if rec.CreatedAt.IsZero() {
			rec.CreatedAt = existing.CreatedAt
		}
		// 若已有记录包含密码/TOTP/Token，而待存记录为空，则合并继承
		if rec.Password == "" && existing.Password != "" {
			rec.Password = existing.Password
			a.Password = existing.Password
		}
		if rec.TOTPSecret == "" && existing.TOTPSecret != "" {
			rec.TOTPSecret = existing.TOTPSecret
			a.TOTPSecret = existing.TOTPSecret
		}
		if rec.RefreshToken == "" && existing.RefreshToken != "" {
			rec.RefreshToken = existing.RefreshToken
			a.RefreshToken = existing.RefreshToken
		}
		if rec.SessionToken == "" && existing.SessionToken != "" {
			rec.SessionToken = existing.SessionToken
			a.SessionToken = existing.SessionToken
		}
		if rec.Token == "" && existing.Token != "" {
			rec.Token = existing.Token
			a.Token = existing.Token
		}
		if rec.Proxy == "" && existing.Proxy != "" {
			rec.Proxy = existing.Proxy
			a.Proxy = existing.Proxy
		}
	} else {
		// 未找到 existing
	}

	return database.Save(rec).Error
}

// BatchSaveAccountsToDB 批量持久化账号列表至数据库（通过内存映射与批量 Upsert，避免 N 次往返造成严重启动延迟）
func BatchSaveAccountsToDB(database *gorm.DB, accs []*Account) error {
	if database == nil || len(accs) == 0 {
		return nil
	}

	// 1. 一次性读取已有账号建立索引映射
	var existingList []db.PoolAccountRecord
	_ = database.Find(&existingList).Error
	existingByID := make(map[string]*db.PoolAccountRecord, len(existingList))
	existingByEmail := make(map[string]*db.PoolAccountRecord, len(existingList))
	existingByChatID := make(map[string]*db.PoolAccountRecord, len(existingList))
	for i := range existingList {
		r := &existingList[i]
		if r.ID != "" {
			existingByID[r.ID] = r
		}
		if r.Email != "" {
			existingByEmail[strings.ToLower(strings.TrimSpace(r.Email))] = r
		}
		if r.ChatGPTAccountID != "" {
			existingByChatID[strings.TrimSpace(r.ChatGPTAccountID)] = r
		}
	}

	// 2. 在内存中合并已有凭据与更新
	recordsToSave := make([]*db.PoolAccountRecord, 0, len(accs))
	for _, a := range accs {
		if a == nil || a.IsTemporary {
			continue
		}
		rec := AccountToRecord(a)
		if rec == nil {
			continue
		}

		var existing *db.PoolAccountRecord
		if ex, ok := existingByID[rec.ID]; ok {
			existing = ex
		} else if rec.Email != "" {
			if ex, ok := existingByEmail[strings.ToLower(strings.TrimSpace(rec.Email))]; ok {
				existing = ex
			}
		} else if rec.ChatGPTAccountID != "" {
			if ex, ok := existingByChatID[strings.TrimSpace(rec.ChatGPTAccountID)]; ok {
				existing = ex
			}
		}

		if existing != nil && existing.ID != "" {
			rec.ID = existing.ID
			a.ID = existing.ID
			if rec.CreatedAt.IsZero() {
				rec.CreatedAt = existing.CreatedAt
			}
			if rec.Password == "" && existing.Password != "" {
				rec.Password = existing.Password
				a.Password = existing.Password
			}
			if rec.TOTPSecret == "" && existing.TOTPSecret != "" {
				rec.TOTPSecret = existing.TOTPSecret
				a.TOTPSecret = existing.TOTPSecret
			}
			if rec.RefreshToken == "" && existing.RefreshToken != "" {
				rec.RefreshToken = existing.RefreshToken
				a.RefreshToken = existing.RefreshToken
			}
			if rec.SessionToken == "" && existing.SessionToken != "" {
				rec.SessionToken = existing.SessionToken
				a.SessionToken = existing.SessionToken
			}
			if rec.Token == "" && existing.Token != "" {
				rec.Token = existing.Token
				a.Token = existing.Token
			}
			if rec.Proxy == "" && existing.Proxy != "" {
				rec.Proxy = existing.Proxy
				a.Proxy = existing.Proxy
			}
		}

		recordsToSave = append(recordsToSave, rec)
	}

	if len(recordsToSave) == 0 {
		return nil
	}

	// 3. 批量 Upsert（支持 SQLite 与 PostgreSQL）
	return database.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).CreateInBatches(recordsToSave, 100).Error
}

// LoadAccountsFromDB 从数据库读取全部号池账号
func LoadAccountsFromDB(database *gorm.DB, profilePool []FingerprintProfile) ([]*Account, error) {
	if database == nil {
		return nil, nil
	}
	var records []db.PoolAccountRecord
	if err := database.Where("provider = ? OR provider = ''", "chatgpt").Order("created_at asc").Find(&records).Error; err != nil {
		return nil, err
	}
	var accountsList []*Account
	for _, rec := range records {
		r := rec
		acct := RecordToAccount(&r, profilePool)
		if acct != nil {
			accountsList = append(accountsList, acct)
		}
	}
	return accountsList, nil
}

// DeleteAccountFromDB 从数据库删除指定账号记录
func DeleteAccountFromDB(database *gorm.DB, id string) error {
	if database == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	cleanID := strings.TrimSpace(id)
	return database.Where("id = ? OR (email != '' AND LOWER(email) = ?) OR (chatgpt_account_id != '' AND chatgpt_account_id = ?)",
		cleanID, strings.ToLower(cleanID), cleanID).Delete(&db.PoolAccountRecord{}).Error
}
