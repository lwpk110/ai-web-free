package handler

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"ai-web-free/internal/db"
	"ai-web-free/internal/storage"

	"gorm.io/gorm"
)

func detectMediaMimeFromExt(ext string, data []byte) string {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	default:
		if len(data) > 0 {
			return http.DetectContentType(data)
		}
		return "application/octet-stream"
	}
}

func extractLocalStorageRelPath(rawURL string) (string, bool) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", false
	}

	pathPart := ""
	if strings.HasPrefix(trimmed, "/storage/") {
		pathPart = trimmed
	} else if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		parsed, err := url.Parse(trimmed)
		if err != nil || !strings.HasPrefix(parsed.Path, "/storage/") {
			return "", false
		}
		host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
		if host != "" && host != "localhost" {
			ip := net.ParseIP(host)
			if ip == nil || (!ip.IsLoopback() && !ip.IsUnspecified()) {
				return "", false
			}
		}
		pathPart = parsed.Path
	} else {
		return "", false
	}

	if idx := strings.IndexAny(pathPart, "?#"); idx >= 0 {
		pathPart = pathPart[:idx]
	}

	relPath := strings.TrimPrefix(pathPart, "/storage/")
	relPath = strings.ReplaceAll(strings.TrimLeft(relPath, "/"), "\\", "/")
	if relPath == "" {
		return "", false
	}
	for _, seg := range strings.Split(relPath, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	return relPath, true
}

// LoadLocalImageAsset 从本地/内存 StorageEngine（及可选 GalleryWork 记录）读取 `/storage/...` 媒体资产
func LoadLocalImageAsset(rawURL string, engine storage.StorageEngine, database *gorm.DB) ([]byte, string, string, bool) {
	relPath, ok := extractLocalStorageRelPath(rawURL)
	if !ok {
		return nil, "", "", false
	}

	filename := filepath.Base(relPath)
	if engine != nil {
		if mse, hasMedia := engine.(storage.MediaStorageEngine); hasMedia && mse != nil {
			if data, err := mse.Read(context.Background(), relPath); err == nil && len(data) > 0 {
				return data, detectMediaMimeFromExt(filepath.Ext(filename), data), filename, true
			}
		}
	}

	if database != nil && strings.HasPrefix(relPath, "images/") {
		baseNoExt := strings.TrimSuffix(filename, filepath.Ext(filename))
		var work db.GalleryWork
		storageURL := "/storage/" + relPath
		if err := database.Where("url = ? OR file_path = ? OR file_path = ? OR id = ?", storageURL, relPath, strings.ReplaceAll(relPath, "/", string(filepath.Separator)), baseNoExt).First(&work).Error; err == nil {
			if work.FilePath != "" && engine != nil {
				if mse, hasMedia := engine.(storage.MediaStorageEngine); hasMedia && mse != nil {
					if data, readErr := mse.Read(context.Background(), work.FilePath); readErr == nil && len(data) > 0 {
						return data, detectMediaMimeFromExt(filepath.Ext(filename), data), filename, true
					}
				}
			}
			if len(work.BlobData) > 0 {
				mt := work.MimeType
				if mt == "" {
					mt = detectMediaMimeFromExt(filepath.Ext(filename), work.BlobData)
				}
				return work.BlobData, mt, filename, true
			}
		}
	}

	return nil, "", "", false
}

// ResolveMediaURLToDataURI 将站内 `/storage/...`（或本机回环 `/storage/...`）URL 转换为可供外部/内部供应商消费的 `data:<mime>;base64,...` URI；非站内资源原样返回。
func ResolveMediaURLToDataURI(rawURL string, engine storage.StorageEngine, database *gorm.DB) string {
	data, mimeType, _, ok := LoadLocalImageAsset(rawURL, engine, database)
	if !ok || len(data) == 0 {
		return rawURL
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}
