package vectorstore

import (
	"context"
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{1.0, 0.0, 0.0},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{0.0, 1.0, 0.0},
			expected: 0.0,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1.0, 0.0},
			b:        []float32{-1.0, 0.0},
			expected: -1.0,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 0.0,
		},
		{
			name:     "different lengths",
			a:        []float32{1.0, 2.0},
			b:        []float32{1.0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			if math.Abs(float64(got-tt.expected)) > 1e-5 {
				t.Errorf("CosineSimilarity() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMemoryVectorStore_Lifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryVectorStore()

	if store.Name() != "memory" {
		t.Errorf("expected store name 'memory', got %s", store.Name())
	}

	docs := []Document{
		{
			ID:      "doc-1",
			Content: "Go 语言并发编程指南",
			Vector:  []float32{1.0, 0.0, 0.0},
			Metadata: map[string]interface{}{
				"tag": "golang",
			},
		},
		{
			ID:      "doc-2",
			Content: "Python 机器学习与数据科学",
			Vector:  []float32{0.0, 1.0, 0.0},
			Metadata: map[string]interface{}{
				"tag": "python",
			},
		},
		{
			ID:      "doc-3",
			Content: "Go 微服务架构实战",
			Vector:  []float32{0.9, 0.1, 0.0},
			Metadata: map[string]interface{}{
				"tag": "golang",
			},
		},
	}

	if err := store.AddDocuments(ctx, docs); err != nil {
		t.Fatalf("AddDocuments failed: %v", err)
	}

	// 检索与 [1, 0, 0] 最近的 Top 2 文档
	query := []float32{1.0, 0.0, 0.0}
	results, err := store.SimilaritySearch(ctx, query, 2)
	if err != nil {
		t.Fatalf("SimilaritySearch failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Document.ID != "doc-1" {
		t.Errorf("expected top result doc-1, got %s", results[0].Document.ID)
	}
	if results[1].Document.ID != "doc-3" {
		t.Errorf("expected second result doc-3, got %s", results[1].Document.ID)
	}

	// 删除 doc-1
	if err := store.DeleteDocuments(ctx, []string{"doc-1"}); err != nil {
		t.Fatalf("DeleteDocuments failed: %v", err)
	}

	resultsAfter, err := store.SimilaritySearch(ctx, query, 2)
	if err != nil {
		t.Fatalf("SimilaritySearch after delete failed: %v", err)
	}

	if len(resultsAfter) != 2 {
		t.Fatalf("expected 2 results after delete, got %d", len(resultsAfter))
	}
	if resultsAfter[0].Document.ID != "doc-3" {
		t.Errorf("expected top result now doc-3, got %s", resultsAfter[0].Document.ID)
	}
}

func TestNewVectorStoreFactory(t *testing.T) {
	store, err := NewVectorStore(Config{Type: "memory"})
	if err != nil || store == nil {
		t.Fatalf("failed to create memory vector store: %v", err)
	}
	if store.Name() != "memory" {
		t.Errorf("expected 'memory', got %s", store.Name())
	}

	noneStore, err := NewVectorStore(Config{Type: "none"})
	if err != nil || noneStore != nil {
		t.Fatalf("expected nil store for 'none', got %v", noneStore)
	}

	_, err = NewVectorStore(Config{Type: "invalid-type"})
	if err == nil {
		t.Fatalf("expected error for unsupported store type")
	}
}
