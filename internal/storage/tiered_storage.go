package storage

import (
	"context"
	"os"
	"strings"
	"time"
)

// TieredStorageEngine 两级分层媒体存储引擎：
// L1: 本地磁盘 (LocalStorageEngine)，低延迟、零出网流量、毫秒级响应
// L2: 七牛云对象存储 (QiniuClient)，持久备份源、跨机器迁移按需回填
type TieredStorageEngine struct {
	local   *LocalStorageEngine
	qiniu   *QiniuClient
	asyncUp bool
}

// NewTieredStorageEngine 构造两级分层媒体存储引擎
func NewTieredStorageEngine(local *LocalStorageEngine, qiniu *QiniuClient) *TieredStorageEngine {
	return &TieredStorageEngine{
		local:   local,
		qiniu:   qiniu,
		asyncUp: true,
	}
}

func (t *TieredStorageEngine) Type() string {
	return "tiered_qiniu"
}

func (t *TieredStorageEngine) BaseDir() string {
	if t.local != nil {
		return t.local.BaseDir()
	}
	return ""
}

func (t *TieredStorageEngine) ImagesDir() string {
	if t.local != nil {
		return t.local.ImagesDir()
	}
	return ""
}

func (t *TieredStorageEngine) VideosDir() string {
	if t.local != nil {
		return t.local.VideosDir()
	}
	return ""
}

func (t *TieredStorageEngine) GetLocalStorage() *LocalStorageEngine {
	return t.local
}

func (t *TieredStorageEngine) GetQiniuClient() *QiniuClient {
	return t.qiniu
}

func (t *TieredStorageEngine) Save(ctx context.Context, id string, data []byte, mimeType string) (string, string, error) {
	return t.SaveInCategory(ctx, "images", id, data, mimeType)
}

func (t *TieredStorageEngine) SaveInCategory(ctx context.Context, category, id string, data []byte, mimeType string) (string, string, error) {
	// 1. 本地快速落盘
	url, relPath, err := t.local.SaveInCategory(ctx, category, id, data, mimeType)
	if err != nil {
		return "", "", err
	}

	// 2. 双写异步备份至七牛云 (Key 固定规范: storage/<category>/<filename>)
	if t.qiniu != nil && len(data) > 0 {
		normalizedRel := strings.ReplaceAll(relPath, "\\", "/")
		qiniuKey := "storage/" + normalizedRel
		if t.asyncUp {
			dataCopy := make([]byte, len(data))
			copy(dataCopy, data)
			go func() {
				uploadCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				_, _ = t.qiniu.Upload(uploadCtx, qiniuKey, dataCopy, mimeType)
			}()
		} else {
			_, _ = t.qiniu.Upload(ctx, qiniuKey, data, mimeType)
		}
	}

	return url, relPath, nil
}

func (t *TieredStorageEngine) Read(ctx context.Context, path string) ([]byte, error) {
	// 1. 优先查本地热点缓存
	if t.local.Exists(ctx, path) {
		return t.local.Read(ctx, path)
	}

	// 2. 本地未命中（换机或本地缓存已清理），从七牛云按需回填
	if t.qiniu != nil {
		normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
		qiniuKey := normalized
		if !strings.HasPrefix(qiniuKey, "storage/") {
			qiniuKey = "storage/" + normalized
		}

		data, err := t.qiniu.Download(ctx, qiniuKey)
		if err == nil && len(data) > 0 {
			// 回写到本地磁盘，下次请求直接命中本地 L1！
			cat, cleanID := splitCategoryAndID(normalized)
			_, _, _ = t.local.SaveInCategory(ctx, cat, cleanID, data, "")
			return data, nil
		}
	}

	return nil, os.ErrNotExist
}

func (t *TieredStorageEngine) Delete(ctx context.Context, path string) error {
	_ = t.local.Delete(ctx, path)
	if t.qiniu != nil {
		normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
		qiniuKey := "storage/" + normalized
		_ = t.qiniu.Delete(ctx, qiniuKey)
	}
	return nil
}

func (t *TieredStorageEngine) Exists(ctx context.Context, path string) bool {
	if t.local.Exists(ctx, path) {
		return true
	}
	if t.qiniu != nil {
		normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
		qiniuKey := "storage/" + normalized
		return t.qiniu.Exists(ctx, qiniuKey)
	}
	return false
}

func splitCategoryAndID(relPath string) (string, string) {
	clean := strings.TrimPrefix(strings.ReplaceAll(relPath, "\\", "/"), "storage/")
	idx := strings.Index(clean, "/")
	if idx <= 0 {
		return "images", clean
	}
	return clean[:idx], clean[idx+1:]
}
