package storage

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// StorageEngine 统一存储引擎接口
type StorageEngine interface {
	Save(ctx context.Context, id string, data []byte, mimeType string) (url string, path string, err error)
	Delete(ctx context.Context, path string) error
	Exists(ctx context.Context, path string) bool
	Type() string
}

// MediaStorageEngine 扩展支持多模态分类目录（images / videos / audio）存储与字节读取。
type MediaStorageEngine interface {
	StorageEngine
	SaveInCategory(ctx context.Context, category, id string, data []byte, mimeType string) (url string, path string, err error)
	Read(ctx context.Context, path string) ([]byte, error)
}

// PrefixLister 可选扩展接口：按前缀一次性列举已存在的对象相对路径，避免逐条 Exists 探测引发 N+1 远程请求。
type PrefixLister interface {
	ListPrefix(ctx context.Context, prefix string) ([]string, error)
}

// LocalPathResolver 可选扩展接口：若对象已命中本地磁盘 L1 缓存，返回物理路径以支持零拷贝流式传输与 HTTP Range (206)。
type LocalPathResolver interface {
	LocalFilePath(path string) (string, bool)
}

func resolveExtForMime(category, mimeType string) string {
	lower := strings.ToLower(strings.TrimSpace(mimeType))
	if idx := strings.Index(lower, ";"); idx > 0 {
		lower = strings.TrimSpace(lower[:idx])
	}
	switch lower {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/png":
		return ".png"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "application/json":
		return ".json"
	}
	if exts, _ := mime.ExtensionsByType(lower); len(exts) > 0 && exts[0] != "" {
		return exts[0]
	}
	switch category {
	case "videos":
		return ".mp4"
	case "audio", "voice_samples":
		return ".mp3"
	case "voices":
		return ".json"
	default:
		return ".png"
	}
}

func normalizeMediaCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "videos", "video":
		return "videos"
	case "audio", "audios":
		return "audio"
	case "voices":
		return "voices"
	case "voice_samples", "voices-samples":
		return "voice_samples"
	default:
		return "images"
	}
}

// LocalStorageEngine 本地文件系统存储引擎
type LocalStorageEngine struct {
	baseDir string
	mu      sync.RWMutex
}

// NewLocalStorageEngine 创建本地文件存储引擎
func NewLocalStorageEngine(baseDir string) (*LocalStorageEngine, error) {
	cleanDir := filepath.Clean(strings.TrimSpace(baseDir))
	if cleanDir == "" || cleanDir == "." {
		cleanDir = "data/storage"
	}
	for _, sub := range []string{"images", "videos"} {
		subDir := filepath.Join(cleanDir, sub)
		if err := os.MkdirAll(subDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create storage directory %q: %w", subDir, err)
		}
	}
	return &LocalStorageEngine{baseDir: cleanDir}, nil
}

func (s *LocalStorageEngine) BaseDir() string {
	return s.baseDir
}

func (s *LocalStorageEngine) ImagesDir() string {
	return filepath.Join(s.baseDir, "images")
}

func (s *LocalStorageEngine) VideosDir() string {
	return filepath.Join(s.baseDir, "videos")
}

func (s *LocalStorageEngine) Type() string {
	return "local"
}

func (s *LocalStorageEngine) Save(ctx context.Context, id string, data []byte, mimeType string) (string, string, error) {
	return s.SaveInCategory(ctx, "images", id, data, mimeType)
}

func (s *LocalStorageEngine) SaveInCategory(ctx context.Context, category, id string, data []byte, mimeType string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanID := filepath.Base(strings.TrimSpace(id))
	if cleanID == "" || cleanID == "." || cleanID == ".." {
		return "", "", fmt.Errorf("invalid file id: %q", id)
	}

	cat := normalizeMediaCategory(category)
	ext := resolveExtForMime(cat, mimeType)

	filename := cleanID
	if !strings.HasSuffix(strings.ToLower(filename), ext) {
		filename += ext
	}

	targetDir := filepath.Join(s.baseDir, cat)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", "", fmt.Errorf("ensure %s dir: %w", cat, err)
	}

	fullPath := filepath.Join(targetDir, filename)
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return "", "", fmt.Errorf("write media file %q: %w", fullPath, err)
	}

	relPath := filepath.Join(cat, filename)
	url := "/storage/" + cat + "/" + filename
	return url, relPath, nil
}

func (s *LocalStorageEngine) Read(ctx context.Context, path string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "" || cleanPath == "." || strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
		return nil, fmt.Errorf("invalid path: %q", path)
	}

	fullPath := filepath.Join(s.baseDir, cleanPath)
	baseClean := filepath.Clean(s.baseDir)
	if fullPath != baseClean && !strings.HasPrefix(fullPath, baseClean+string(os.PathSeparator)) {
		return nil, fmt.Errorf("path traversal forbidden: %q", path)
	}

	return os.ReadFile(fullPath)
}

func (s *LocalStorageEngine) Delete(ctx context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "" || cleanPath == "." || strings.HasPrefix(cleanPath, "..") {
		return fmt.Errorf("invalid path: %q", path)
	}

	fullPath := filepath.Join(s.baseDir, cleanPath)
	// 验证绝对路径是否在 baseDir 范围内，防止目录穿越
	if !strings.HasPrefix(fullPath, s.baseDir) {
		return fmt.Errorf("path traversal forbidden: %q", path)
	}

	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *LocalStorageEngine) LocalFilePath(path string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "" || cleanPath == "." || strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
		return "", false
	}
	fullPath := filepath.Join(s.baseDir, cleanPath)
	baseClean := filepath.Clean(s.baseDir)
	if fullPath != baseClean && !strings.HasPrefix(fullPath, baseClean+string(os.PathSeparator)) {
		return "", false
	}
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return "", false
	}
	return fullPath, true
}

func (s *LocalStorageEngine) Exists(ctx context.Context, path string) bool {
	_, ok := s.LocalFilePath(path)
	return ok
}

func (s *LocalStorageEngine) ListPrefix(ctx context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cleanPrefix := strings.Trim(strings.ReplaceAll(strings.TrimSpace(prefix), "\\", "/"), "/")
	if cleanPrefix == "" || strings.Contains(cleanPrefix, "..") {
		return nil, nil
	}
	dir := filepath.Join(s.baseDir, filepath.FromSlash(cleanPrefix))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		info, err := ent.Info()
		if err != nil || info.Size() <= 0 {
			continue
		}
		out = append(out, cleanPrefix+"/"+ent.Name())
	}
	return out, nil
}

// MemoryStorageEngine 纯内存临时存储引擎（用于降级或单元测试）
type MemoryStorageEngine struct {
	files map[string][]byte
	mu    sync.RWMutex
}

func NewMemoryStorageEngine() *MemoryStorageEngine {
	return &MemoryStorageEngine{
		files: make(map[string][]byte),
	}
}

func (m *MemoryStorageEngine) Type() string {
	return "memory"
}

func (m *MemoryStorageEngine) Save(ctx context.Context, id string, data []byte, mimeType string) (string, string, error) {
	return m.SaveInCategory(ctx, "images", id, data, mimeType)
}

func (m *MemoryStorageEngine) SaveInCategory(ctx context.Context, category, id string, data []byte, mimeType string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanID := filepath.Base(strings.TrimSpace(id))
	if cleanID == "" || cleanID == "." || cleanID == ".." {
		return "", "", fmt.Errorf("invalid file id: %q", id)
	}
	cat := normalizeMediaCategory(category)
	ext := resolveExtForMime(cat, mimeType)
	filename := cleanID
	if !strings.HasSuffix(strings.ToLower(filename), ext) {
		filename += ext
	}
	relPath := cat + "/" + filename
	cp := make([]byte, len(data))
	copy(cp, data)
	m.files[relPath] = cp
	return "/storage/" + relPath, relPath, nil
}

func (m *MemoryStorageEngine) Read(ctx context.Context, path string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	data, ok := m.files[normalized]
	if !ok {
		return nil, os.ErrNotExist
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp, nil
}

func (m *MemoryStorageEngine) Delete(ctx context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, path)
	return nil
}

func (m *MemoryStorageEngine) Exists(ctx context.Context, path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.files[path]
	return ok && len(data) > 0
}

func (m *MemoryStorageEngine) ListPrefix(ctx context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cleanPrefix := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(prefix), "\\", "/"), "/")
	out := make([]string, 0)
	for k, v := range m.files {
		if len(v) > 0 && strings.HasPrefix(k, cleanPrefix) {
			out = append(out, k)
		}
	}
	return out, nil
}
