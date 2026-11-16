package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
)

// QiniuConfig 七牛云对象存储配置
type QiniuConfig struct {
	AccessKey string
	SecretKey string
	Bucket    string
	Domain    string
	Region    string
	UploadURL string
}

// QiniuClient 纯 Go 七牛云 Kodo API 客户端（严格零外部依赖，CGO_ENABLED=0 友好）
type QiniuClient struct {
	cfg        QiniuConfig
	httpClient *http.Client
}

// NewQiniuClient 创建七牛云客户端实例
func NewQiniuClient(cfg QiniuConfig) *QiniuClient {
	if cfg.UploadURL == "" {
		cfg.UploadURL = "https://upload.qiniup.com"
	}
	cfg.Domain = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(cfg.Domain), "http://"), "https://")
	return &QiniuClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (q *QiniuClient) signQBox(data string) string {
	mac := hmac.New(sha1.New, []byte(q.cfg.SecretKey))
	mac.Write([]byte(data))
	sign := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("QBox %s:%s", q.cfg.AccessKey, sign)
}

func (q *QiniuClient) makeUploadToken(key string) string {
	type PutPolicy struct {
		Scope    string `json:"scope"`
		Deadline int64  `json:"deadline"`
	}
	scope := q.cfg.Bucket
	if key != "" {
		scope = fmt.Sprintf("%s:%s", q.cfg.Bucket, key)
	}
	policy := PutPolicy{
		Scope:    scope,
		Deadline: time.Now().Add(2 * time.Hour).Unix(),
	}
	data, _ := json.Marshal(policy)
	encodedPolicy := base64.URLEncoding.EncodeToString(data)

	mac := hmac.New(sha1.New, []byte(q.cfg.SecretKey))
	mac.Write([]byte(encodedPolicy))
	encodedSign := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s:%s:%s", q.cfg.AccessKey, encodedSign, encodedPolicy)
}

func (q *QiniuClient) makePrivateDownloadURL(rawURL string) string {
	deadline := time.Now().Add(2 * time.Hour).Unix()
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	target := fmt.Sprintf("%s%se=%d", rawURL, sep, deadline)
	mac := hmac.New(sha1.New, []byte(q.cfg.SecretKey))
	mac.Write([]byte(target))
	sign := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s&token=%s:%s", target, q.cfg.AccessKey, sign)
}

// Upload 上传文件字节至指定 key
func (q *QiniuClient) Upload(ctx context.Context, key string, data []byte, mimeType string) (string, error) {
	cleanKey := strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(key), "/"), "\\", "/")
	upToken := q.makeUploadToken(cleanKey)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("token", upToken); err != nil {
		return "", err
	}
	if err := writer.WriteField("key", cleanKey); err != nil {
		return "", err
	}
	filename := cleanKey
	if idx := strings.LastIndex(cleanKey, "/"); idx >= 0 {
		filename = cleanKey[idx+1:]
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", q.cfg.UploadURL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("qiniu upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("qiniu upload status %d: %s", resp.StatusCode, string(respBytes))
	}

	if q.cfg.Domain != "" {
		return fmt.Sprintf("http://%s/%s", q.cfg.Domain, cleanKey), nil
	}
	return cleanKey, nil
}

// Download 从七牛云拉取指定 key 的完整二进制内容
func (q *QiniuClient) Download(ctx context.Context, key string) ([]byte, error) {
	if q.cfg.Domain == "" {
		return nil, fmt.Errorf("qiniu domain is not configured")
	}
	cleanKey := strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(key), "/"), "\\", "/")
	rawURL := fmt.Sprintf("http://%s/%s", q.cfg.Domain, cleanKey)
	downloadURL := q.makePrivateDownloadURL(rawURL)

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qiniu download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, os.ErrNotExist
	}
	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("qiniu download status %d: %s", resp.StatusCode, string(respBytes))
	}

	return io.ReadAll(resp.Body)
}

// Exists 探测指定 key 是否在七牛云存储桶中存在
func (q *QiniuClient) Exists(ctx context.Context, key string) bool {
	cleanKey := strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(key), "/"), "\\", "/")
	entryURI := base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", q.cfg.Bucket, cleanKey)))
	statPath := fmt.Sprintf("/stat/%s\n", entryURI)
	statURL := fmt.Sprintf("https://rs.qbox.me/stat/%s", entryURI)

	req, err := http.NewRequestWithContext(ctx, "GET", statURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", q.signQBox(statPath))

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Delete 从七牛云删除指定 key
func (q *QiniuClient) Delete(ctx context.Context, key string) error {
	cleanKey := strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(key), "/"), "\\", "/")
	entryURI := base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", q.cfg.Bucket, cleanKey)))
	delPath := fmt.Sprintf("/delete/%s\n", entryURI)
	delURL := fmt.Sprintf("https://rs.qbox.me/delete/%s", entryURI)

	req, err := http.NewRequestWithContext(ctx, "POST", delURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", q.signQBox(delPath))

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == 612 /* 资源已不存在 */ {
		return nil
	}
	respBytes, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("qiniu delete status %d: %s", resp.StatusCode, string(respBytes))
}

// List 列举特定前缀的对象列表
func (q *QiniuClient) List(ctx context.Context, prefix string, limit int) ([]string, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	pathWithQuery := fmt.Sprintf("/list?bucket=%s&prefix=%s&limit=%d", q.cfg.Bucket, prefix, limit)
	listURL := "https://rsf.qbox.me" + pathWithQuery

	req, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", q.signQBox(pathWithQuery+"\n"))

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("qiniu list status %d: %s", resp.StatusCode, string(respBytes))
	}

	var res struct {
		Items []struct {
			Key string `json:"key"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(res.Items))
	for _, it := range res.Items {
		keys = append(keys, it.Key)
	}
	return keys, nil
}
