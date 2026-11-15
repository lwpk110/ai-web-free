package vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Document 向量存储基础文档单元
type Document struct {
	ID       string                 `json:"id"`
	Content  string                 `json:"content"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Vector   []float32              `json:"vector,omitempty"`
}

// SearchResult 向量召回结果
type SearchResult struct {
	Document Document `json:"document"`
	Score    float32  `json:"score"`
}

// VectorStore 统一向量库驱动接口（严格零 CGO）
type VectorStore interface {
	Name() string
	AddDocuments(ctx context.Context, docs []Document) error
	SimilaritySearch(ctx context.Context, queryVector []float32, topK int) ([]SearchResult, error)
	DeleteDocuments(ctx context.Context, ids []string) error
}

// CosineSimilarity 计算两个等长向量的余弦相似度，范围 [-1, 1]
func CosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dotProduct float64
	var normA float64
	var normB float64
	for i := range a {
		va := float64(a[i])
		vb := float64(b[i])
		dotProduct += va * vb
		normA += va * va
		normB += vb * vb
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// ─── 纯内存零依赖向量存储驱动 (MemoryVectorStore) ─────────────────────────

// MemoryVectorStore 纯 Go 内存并发安全向量存储（单二进制默认开箱即用，零依赖）
type MemoryVectorStore struct {
	mu   sync.RWMutex
	docs map[string]Document
}

// NewMemoryVectorStore 实例化内存向量库
func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{
		docs: make(map[string]Document),
	}
}

func (s *MemoryVectorStore) Name() string {
	return "memory"
}

func (s *MemoryVectorStore) AddDocuments(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, doc := range docs {
		if doc.ID == "" {
			continue
		}
		s.docs[doc.ID] = doc
	}
	return nil
}

func (s *MemoryVectorStore) SimilaritySearch(ctx context.Context, queryVector []float32, topK int) ([]SearchResult, error) {
	if len(queryVector) == 0 || topK <= 0 {
		return []SearchResult{}, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	type scoredDoc struct {
		doc   Document
		score float32
	}

	scored := make([]scoredDoc, 0, len(s.docs))
	for _, doc := range s.docs {
		if len(doc.Vector) == 0 {
			continue
		}
		score := CosineSimilarity(queryVector, doc.Vector)
		scored = append(scored, scoredDoc{doc: doc, score: score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	if topK > len(scored) {
		topK = len(scored)
	}

	results := make([]SearchResult, topK)
	for i := 0; i < topK; i++ {
		results[i] = SearchResult{
			Document: scored[i].doc,
			Score:    scored[i].score,
		}
	}
	return results, nil
}

func (s *MemoryVectorStore) DeleteDocuments(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range ids {
		delete(s.docs, id)
	}
	return nil
}

// ─── Qdrant HTTP REST 客户端驱动 (QdrantVectorStore) ──────────────────────

// QdrantConfig Qdrant REST 连接配置
type QdrantConfig struct {
	URL        string
	APIKey     string
	Collection string
	Timeout    time.Duration
}

// QdrantVectorStore 基于纯 HTTP REST 通信的 Qdrant 客户端（严格零 CGO）
type QdrantVectorStore struct {
	cfg    QdrantConfig
	client *http.Client
}

// NewQdrantVectorStore 实例化 Qdrant REST 驱动
func NewQdrantVectorStore(cfg QdrantConfig) *QdrantVectorStore {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if cfg.Collection == "" {
		cfg.Collection = "ai_web_free_knowledge"
	}
	return &QdrantVectorStore{
		cfg:    cfg,
		client: &http.Client{Timeout: timeout},
	}
}

func (q *QdrantVectorStore) Name() string {
	return "qdrant"
}

func (q *QdrantVectorStore) AddDocuments(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}
	endpoint := fmt.Sprintf("%s/collections/%s/points?wait=true", strings.TrimRight(q.cfg.URL, "/"), q.cfg.Collection)

	type point struct {
		ID      string                 `json:"id"`
		Vector  []float32              `json:"vector"`
		Payload map[string]interface{} `json:"payload"`
	}

	points := make([]point, 0, len(docs))
	for _, doc := range docs {
		payload := map[string]interface{}{
			"content": doc.Content,
		}
		for k, v := range doc.Metadata {
			payload[k] = v
		}
		points = append(points, point{
			ID:      doc.ID,
			Vector:  doc.Vector,
			Payload: payload,
		})
	}

	bodyData, err := json.Marshal(map[string]interface{}{"points": points})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(bodyData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if q.cfg.APIKey != "" {
		req.Header.Set("api-key", q.cfg.APIKey)
	}

	resp, err := q.client.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant add points: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant returned status %d", resp.StatusCode)
	}
	return nil
}

func (q *QdrantVectorStore) SimilaritySearch(ctx context.Context, queryVector []float32, topK int) ([]SearchResult, error) {
	if len(queryVector) == 0 || topK <= 0 {
		return []SearchResult{}, nil
	}
	endpoint := fmt.Sprintf("%s/collections/%s/points/search", strings.TrimRight(q.cfg.URL, "/"), q.cfg.Collection)

	payload := map[string]interface{}{
		"vector":       queryVector,
		"limit":        topK,
		"with_payload": true,
		"with_vector":  false,
	}

	bodyData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if q.cfg.APIKey != "" {
		req.Header.Set("api-key", q.cfg.APIKey)
	}

	resp, err := q.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qdrant search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("qdrant search returned status %d", resp.StatusCode)
	}

	var searchResp struct {
		Result []struct {
			ID      interface{}            `json:"id"`
			Score   float32                `json:"score"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("decode qdrant response: %w", err)
	}

	results := make([]SearchResult, 0, len(searchResp.Result))
	for _, item := range searchResp.Result {
		idStr := fmt.Sprintf("%v", item.ID)
		content := ""
		if c, ok := item.Payload["content"].(string); ok {
			content = c
		}
		results = append(results, SearchResult{
			Document: Document{
				ID:       idStr,
				Content:  content,
				Metadata: item.Payload,
			},
			Score: item.Score,
		})
	}
	return results, nil
}

func (q *QdrantVectorStore) DeleteDocuments(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	endpoint := fmt.Sprintf("%s/collections/%s/points/delete?wait=true", strings.TrimRight(q.cfg.URL, "/"), q.cfg.Collection)

	bodyData, err := json.Marshal(map[string]interface{}{
		"points": ids,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if q.cfg.APIKey != "" {
		req.Header.Set("api-key", q.cfg.APIKey)
	}

	resp, err := q.client.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant delete points: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant delete returned status %d", resp.StatusCode)
	}
	return nil
}

// ─── 工厂方法 ────────────────────────────────────────────────────────────

// Config 向量库工厂初始化配置
type Config struct {
	Type       string // "memory", "qdrant", "none"
	QdrantURL  string
	QdrantKey  string
	Collection string
}

// NewVectorStore 统一构造工厂
func NewVectorStore(cfg Config) (VectorStore, error) {
	vType := strings.ToLower(strings.TrimSpace(cfg.Type))
	switch vType {
	case "", "memory", "builtin":
		return NewMemoryVectorStore(), nil
	case "qdrant":
		if cfg.QdrantURL == "" {
			return nil, errors.New("qdrant url is required when VECTOR_STORE_TYPE=qdrant")
		}
		return NewQdrantVectorStore(QdrantConfig{
			URL:        cfg.QdrantURL,
			APIKey:     cfg.QdrantKey,
			Collection: cfg.Collection,
		}), nil
	case "none":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported vector store type: %q", cfg.Type)
	}
}
