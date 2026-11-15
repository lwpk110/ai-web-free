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
	imagesDir := filepath.Join(cleanDir, "images")
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory %q: %w", imagesDir, err)
	}
	return &LocalStorageEngine{baseDir: cleanDir}, nil
}

func (s *LocalStorageEngine) BaseDir() string {
	return s.baseDir
}

func (s *LocalStorageEngine) ImagesDir() string {
	return filepath.Join(s.baseDir, "images")
}

func (s *LocalStorageEngine) Type() string {
	return "local"
}

func (s *LocalStorageEngine) Save(ctx context.Context, id string, data []byte, mimeType string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanID := filepath.Base(strings.TrimSpace(id))
	if cleanID == "" || cleanID == "." {
		return "", "", fmt.Errorf("invalid file id: %q", id)
	}

	ext := ".png"
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	default:
		exts, _ := mime.ExtensionsByType(mimeType)
		if len(exts) > 0 && exts[0] != "" {
			ext = exts[0]
		}
	}

	filename := cleanID
	if !strings.HasSuffix(filename, ext) {
		filename += ext
	}

	imagesDir := s.ImagesDir()
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return "", "", fmt.Errorf("ensure images dir: %w", err)
	}

	fullPath := filepath.Join(imagesDir, filename)
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return "", "", fmt.Errorf("write image file %q: %w", fullPath, err)
	}

	relPath := filepath.Join("images", filename)
	url := "/storage/images/" + filename
	return url, relPath, nil
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

func (s *LocalStorageEngine) Exists(ctx context.Context, path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "" || cleanPath == "." || strings.HasPrefix(cleanPath, "..") {
		return false
	}
	fullPath := filepath.Join(s.baseDir, cleanPath)
	info, err := os.Stat(fullPath)
	return err == nil && !info.IsDir() && info.Size() > 0
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
	m.mu.Lock()
	defer m.mu.Unlock()

	filename := id + ".png"
	relPath := "images/" + filename
	m.files[relPath] = data
	return "/storage/images/" + filename, relPath, nil
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
