package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"ai-web-free/internal/storage"
)

const voiceCatalogID = "catalog"

// VoiceCatalogFile 本地持久化的音色目录快照。
type VoiceCatalogFile struct {
	UpdatedAt time.Time         `json:"updated_at"`
	Voices    []VoiceDescriptor `json:"voices"`
}

// VoiceCatalogStore 管理本地音色目录快照（voices/catalog.json）与试听样音缓存（voice_samples/*.mp3）。
type VoiceCatalogStore struct {
	media storage.MediaStorageEngine
}

// NewVoiceCatalogStore 基于统一 StorageEngine 构建音色与试听样音缓存库（在无持久化引擎时安全降级）。
func NewVoiceCatalogStore(eng storage.StorageEngine) *VoiceCatalogStore {
	if eng == nil {
		return &VoiceCatalogStore{}
	}
	media, _ := eng.(storage.MediaStorageEngine)
	return &VoiceCatalogStore{media: media}
}

// Enabled 返回当前存储后端是否支持字节读取与分类保存。
func (s *VoiceCatalogStore) Enabled() bool {
	return s != nil && s.media != nil
}

// SanitizeVoiceID 将任意音色 ID 规范化为安全文件名片段，避免路径穿越或 Windows 非法字符。
func SanitizeVoiceID(voiceID string) string {
	trimmed := strings.TrimSpace(voiceID)
	if trimmed == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

// SampleRelPath 返回音色试听样音在 StorageEngine 中的相对路径（固定使用正斜杠）。
func (s *VoiceCatalogStore) SampleRelPath(voiceID string) string {
	return "voice_samples/" + SanitizeVoiceID(voiceID) + ".mp3"
}

// SamplePreviewURL 返回面向前端/API 的试听音频接口地址（对含斜杠的模型前缀 ID 自动使用查询参数路由，避免代理还原 %2F）。
func SamplePreviewURL(voiceID string) string {
	trimmed := strings.TrimSpace(voiceID)
	if strings.Contains(trimmed, "/") {
		return "/v1/audio/voice-preview?id=" + url.QueryEscape(trimmed)
	}
	return "/v1/audio/voices/" + url.PathEscape(trimmed) + "/preview"
}

// HasSample 检查指定音色的试听 MP3 是否已缓存在本地存储中。
func (s *VoiceCatalogStore) HasSample(ctx context.Context, voiceID string) bool {
	if !s.Enabled() || strings.TrimSpace(voiceID) == "" {
		return false
	}
	return s.media.Exists(ctx, s.SampleRelPath(voiceID))
}

// ReadSample 读取本地缓存的音色试听 MP3。
func (s *VoiceCatalogStore) ReadSample(ctx context.Context, voiceID string) ([]byte, bool, error) {
	if !s.Enabled() {
		return nil, false, nil
	}
	rel := s.SampleRelPath(voiceID)
	if !s.media.Exists(ctx, rel) {
		return nil, false, nil
	}
	data, err := s.media.Read(ctx, rel)
	if err != nil {
		return nil, false, err
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	return data, true, nil
}

// SaveSample 将合成的音色试听 MP3 持久化写入本地存储。
func (s *VoiceCatalogStore) SaveSample(ctx context.Context, voiceID string, audio []byte) (string, error) {
	if !s.Enabled() {
		return "", errors.New("voice cache storage engine unavailable")
	}
	if len(audio) == 0 {
		return "", errors.New("cannot cache empty voice sample audio")
	}
	_, relPath, err := s.media.SaveInCategory(ctx, "voice_samples", SanitizeVoiceID(voiceID), audio, "audio/mpeg")
	return relPath, err
}

// SaveCatalog 持久化当前可用音色目录快照至 voices/catalog.json。
func (s *VoiceCatalogStore) SaveCatalog(ctx context.Context, voices []VoiceDescriptor) error {
	if !s.Enabled() {
		return errors.New("voice catalog storage engine unavailable")
	}
	payload, err := json.MarshalIndent(VoiceCatalogFile{
		UpdatedAt: time.Now().UTC(),
		Voices:    voices,
	}, "", "  ")
	if err != nil {
		return err
	}
	_, _, err = s.media.SaveInCategory(ctx, "voices", voiceCatalogID, payload, "application/json")
	return err
}

// LoadCatalog 读取本地持久化的音色目录快照；未找到时返回 nil。
func (s *VoiceCatalogStore) LoadCatalog(ctx context.Context) ([]VoiceDescriptor, time.Time, error) {
	if !s.Enabled() {
		return nil, time.Time{}, nil
	}
	raw, err := s.media.Read(ctx, "voices/"+voiceCatalogID+".json")
	if err != nil {
		return nil, time.Time{}, err
	}
	var parsed VoiceCatalogFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, time.Time{}, err
	}
	return parsed.Voices, parsed.UpdatedAt, nil
}

// AnnotateDescriptors 为音色列表补充 cached_local 与 preview_url 元数据并统计已缓存数量。
func (s *VoiceCatalogStore) AnnotateDescriptors(ctx context.Context, voices []VoiceDescriptor) ([]VoiceDescriptor, int) {
	out := make([]VoiceDescriptor, len(voices))
	copy(out, voices)

	var cachedSet map[string]struct{}
	if s.Enabled() {
		if lister, ok := s.media.(storage.PrefixLister); ok {
			if items, err := lister.ListPrefix(ctx, "voice_samples/"); err == nil {
				cachedSet = make(map[string]struct{}, len(items))
				for _, item := range items {
					norm := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(item), "\\", "/"), "storage/")
					cachedSet[norm] = struct{}{}
				}
			}
		}
	}

	cachedCount := 0
	for i := range out {
		if out[i].PreviewURL == "" {
			out[i].PreviewURL = SamplePreviewURL(out[i].ID)
		}
		var has bool
		if cachedSet != nil {
			_, has = cachedSet[s.SampleRelPath(out[i].ID)]
		} else {
			has = s.HasSample(ctx, out[i].ID)
		}
		if has {
			out[i].CachedLocal = true
			cachedCount++
		}
	}
	return out, cachedCount
}
