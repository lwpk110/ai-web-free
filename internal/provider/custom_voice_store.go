package provider

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/db"
	"gorm.io/gorm/clause"
)

const customVoicesStorageID = "custom_voices"

// CustomVoiceRecord 记录通过 Chat 爬虫/手动导入并缓存到本地的系统自定义与克隆音色，支持跨工坊长期复用。
type CustomVoiceRecord struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Role          string    `json:"role,omitempty"`
	Category      string    `json:"category,omitempty"`
	Gender        string    `json:"gender,omitempty"`
	Locale        string    `json:"locale,omitempty"`
	Transcript    string    `json:"transcript,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
	SourceURL     string    `json:"source_url,omitempty"`
	SourcePage    string    `json:"source_page,omitempty"`
	SampleExt     string    `json:"sample_ext,omitempty"`
	SampleBytes   int       `json:"sample_bytes,omitempty"`
	CloneEngine   string    `json:"clone_engine,omitempty"`  // "siliconflow" | "dashscope" | ""
	ClonedVoiceID string    `json:"cloned_voice_id,omitempty"`
	CloneStatus   string    `json:"clone_status"` // "cloned" | "cached_pending_key" | "clone_failed"
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type customVoicesFile struct {
	UpdatedAt time.Time           `json:"updated_at"`
	Voices    []CustomVoiceRecord `json:"voices"`
}

var (
	customVoiceMemMu    sync.RWMutex
	customVoiceMemCache = make(map[string][]CustomVoiceRecord)
)

// BuildSystemCustomVoiceID 生成规范、稳定的系统自定义音色 ID（形如 `custom_<slug>`）。
func BuildSystemCustomVoiceID(nameOrURL string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(nameOrURL)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == '-' || r == '_' || r == ' ' {
			b.WriteByte('_')
		}
		if b.Len() >= 20 {
			break
		}
	}
	slug := strings.Trim(b.String(), "_")
	sum := sha1.Sum([]byte(strings.TrimSpace(nameOrURL) + "|" + time.Now().UTC().Format("20060102150405.000")))
	suffix := hex.EncodeToString(sum[:])[:6]
	if slug == "" {
		return "custom_" + suffix
	}
	return fmt.Sprintf("custom_%s_%s", slug, suffix)
}

func (s *VoiceCatalogStore) memKey() string {
	if s == nil {
		return "default"
	}
	return fmt.Sprintf("%p", s.media)
}

// LoadCustomVoices 从持久化目录（或进程内存兜底）读取全部自定义/克隆音色列表。
func (s *VoiceCatalogStore) LoadCustomVoices(ctx context.Context) ([]CustomVoiceRecord, error) {
	if s != nil && s.Enabled() {
		raw, err := s.media.Read(ctx, "voices/"+customVoicesStorageID+".json")
		if err == nil && len(raw) > 0 {
			var parsed customVoicesFile
			if json.Unmarshal(raw, &parsed) == nil {
				return parsed.Voices, nil
			}
		}
	}
	customVoiceMemMu.RLock()
	list := customVoiceMemCache[s.memKey()]
	out := make([]CustomVoiceRecord, len(list))
	copy(out, list)
	customVoiceMemMu.RUnlock()

	// 内存与文件均为空时，尝试从 SQLite 数据库恢复
	if len(out) == 0 {
		if gdb := db.GetDB(); gdb != nil {
			var dbRecords []db.CustomVoiceModelRecord
			if err := gdb.Order("created_at asc").Find(&dbRecords).Error; err == nil && len(dbRecords) > 0 {
				for _, r := range dbRecords {
					tags := []string{}
					if r.TagsJSON != "" {
						tags = strings.Split(r.TagsJSON, ",")
					}
					out = append(out, CustomVoiceRecord{
						ID:            r.ID,
						Name:          r.Name,
						Role:          r.Role,
						Category:      r.Category,
						Gender:        r.Gender,
						Locale:        r.Locale,
						Transcript:    r.Transcript,
						Tags:          tags,
						SourceURL:     r.SourceURL,
						SourcePage:    r.SourcePage,
						SampleExt:     r.SampleExt,
						SampleBytes:   r.SampleBytes,
						CloneEngine:   r.CloneEngine,
						ClonedVoiceID: r.ClonedVoiceID,
						CloneStatus:   r.CloneStatus,
						Error:         r.Error,
						CreatedAt:     r.CreatedAt,
						UpdatedAt:     r.UpdatedAt,
					})
				}
				customVoiceMemMu.Lock()
				customVoiceMemCache[s.memKey()] = out
				customVoiceMemMu.Unlock()
			}
		}
	}
	return out, nil
}

// SaveCustomVoices 持久化保存自定义/克隆音色列表至存储引擎与 SQLite 数据库。
func (s *VoiceCatalogStore) SaveCustomVoices(ctx context.Context, list []CustomVoiceRecord) error {
	customVoiceMemMu.Lock()
	cp := make([]CustomVoiceRecord, len(list))
	copy(cp, list)
	customVoiceMemCache[s.memKey()] = cp
	customVoiceMemMu.Unlock()

	// 同步持久化写入 SQLite / PostgreSQL 数据库
	if gdb := db.GetDB(); gdb != nil && len(cp) > 0 {
		records := make([]db.CustomVoiceModelRecord, 0, len(cp))
		for _, v := range cp {
			records = append(records, db.CustomVoiceModelRecord{
				ID:            v.ID,
				Name:          v.Name,
				Role:          v.Role,
				Category:      v.Category,
				Gender:        v.Gender,
				Locale:        v.Locale,
				Transcript:    v.Transcript,
				TagsJSON:      strings.Join(v.Tags, ","),
				SourceURL:     v.SourceURL,
				SourcePage:    v.SourcePage,
				SampleExt:     v.SampleExt,
				SampleBytes:   v.SampleBytes,
				CloneEngine:   v.CloneEngine,
				ClonedVoiceID: v.ClonedVoiceID,
				CloneStatus:   v.CloneStatus,
				Error:         v.Error,
				CreatedAt:     v.CreatedAt,
				UpdatedAt:     v.UpdatedAt,
			})
		}
		if len(records) > 0 {
			_ = gdb.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(records, 100).Error
		}
	}

	if s == nil || !s.Enabled() {
		return nil
	}
	payload, err := json.MarshalIndent(customVoicesFile{
		UpdatedAt: time.Now().UTC(),
		Voices:    cp,
	}, "", "  ")
	if err != nil {
		return err
	}
	_, _, err = s.media.SaveInCategory(ctx, "voices", customVoicesStorageID, payload, "application/json")
	return err
}

// UpsertCustomVoice 新增或更新一条自定义/克隆音色记录。
func (s *VoiceCatalogStore) UpsertCustomVoice(ctx context.Context, rec CustomVoiceRecord) (CustomVoiceRecord, error) {
	list, _ := s.LoadCustomVoices(ctx)
	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	if rec.Category == "" {
		rec.Category = "系统克隆库"
	}
	if rec.Gender == "" {
		rec.Gender = "female"
	}
	if rec.Locale == "" {
		rec.Locale = "zh-CN"
	}
	rec.Tags = mergeUniqueVoiceTags(rec.Tags, "系统克隆库", "爬虫采集")
	if rec.Role != "" {
		rec.Tags = mergeUniqueVoiceTags(rec.Tags, "短剧常用", rec.Role)
	}

	replaced := false
	for i := range list {
		if strings.EqualFold(list[i].ID, rec.ID) {
			rec.CreatedAt = list[i].CreatedAt
			list[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		list = append([]CustomVoiceRecord{rec}, list...)
	}
	if err := s.SaveCustomVoices(ctx, list); err != nil {
		return rec, err
	}
	return rec, nil
}

// FindCustomVoice 根据系统 ID（`custom_...`）或云端克隆 ID（`speech:...`）查找自定义音色。
func (s *VoiceCatalogStore) FindCustomVoice(ctx context.Context, voiceID string) (CustomVoiceRecord, bool) {
	target := strings.TrimSpace(voiceID)
	if target == "" {
		return CustomVoiceRecord{}, false
	}
	list, _ := s.LoadCustomVoices(ctx)
	for _, rec := range list {
		if strings.EqualFold(rec.ID, target) || (rec.ClonedVoiceID != "" && strings.EqualFold(rec.ClonedVoiceID, target)) {
			return rec, true
		}
	}
	return CustomVoiceRecord{}, false
}

// DeleteCustomVoice 从系统自定义音色库删除记录。
func (s *VoiceCatalogStore) DeleteCustomVoice(ctx context.Context, voiceID string) (bool, error) {
	target := strings.TrimSpace(voiceID)
	if target == "" {
		return false, nil
	}
	list, _ := s.LoadCustomVoices(ctx)
	filtered := make([]CustomVoiceRecord, 0, len(list))
	removed := false
	for _, rec := range list {
		if strings.EqualFold(rec.ID, target) {
			removed = true
			continue
		}
		filtered = append(filtered, rec)
	}
	if !removed {
		return false, nil
	}
	return true, s.SaveCustomVoices(ctx, filtered)
}

// CustomVoiceToDescriptor 将系统自定义/克隆音色转换为统一 VoiceDescriptor，供各工坊（TTS/短剧）列表复用。
func CustomVoiceToDescriptor(rec CustomVoiceRecord, sfAvailable, dsAvailable bool) VoiceDescriptor {
	tags := mergeUniqueVoiceTags(rec.Tags, "系统克隆库")
	reason := ""
	available := false
	switch rec.CloneStatus {
	case "cloned":
		switch rec.CloneEngine {
		case "siliconflow":
			available = sfAvailable
			tags = mergeUniqueVoiceTags(tags, "硅基云端克隆")
			if !sfAvailable {
				reason = "需要配置 SILICONFLOW_API_KEY 以使用此云端克隆音色"
			}
		case "dashscope":
			available = dsAvailable
			tags = mergeUniqueVoiceTags(tags, "百炼云端克隆")
			if !dsAvailable {
				reason = "需要配置 DASHSCOPE_API_KEY 以使用此云端克隆音色"
			}
		default:
			available = rec.ClonedVoiceID != ""
		}
	default:
		available = false
		tags = mergeUniqueVoiceTags(tags, "待云端复刻")
		reason = rec.Error
		if reason == "" {
			reason = "样音已缓存至本地，点击「云端复刻」并配置 SILICONFLOW_API_KEY 或 DASHSCOPE_API_KEY 后即可合成台词"
		}
	}

	displayName := rec.Name
	if rec.Role != "" && !strings.Contains(displayName, rec.Role) {
		displayName = displayName + " [" + rec.Role + "]"
	}
	sampleText := strings.TrimSpace(rec.Transcript)
	if sampleText == "" {
		sampleText = "你好，这是从网页搜索采集并缓存到系统库的参考音色。"
	}

	return VoiceDescriptor{
		ID:           rec.ID,
		Name:         displayName,
		Provider:     "custom",
		Category:     "系统克隆库",
		Gender:       rec.Gender,
		Locale:       rec.Locale,
		SampleText:   sampleText,
		Tags:         tags,
		PreviewURL:   SamplePreviewURL(rec.ID),
		Available:    available,
		CachedLocal:  rec.SampleBytes > 0,
		StatusReason: reason,
	}
}
