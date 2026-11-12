package web

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedOpenUIAssetsAndSystemPrompt(t *testing.T) {
	prompt := OpenUISystemPrompt()
	if len(prompt) < 1000 {
		t.Fatalf("expected non-empty OpenUI system prompt, got length %d", len(prompt))
	}
	for _, comp := range []string{"PoolOverviewCard", "AccountDiagnosticTable", "ImageStudioCard"} {
		if !strings.Contains(prompt, comp) {
			t.Errorf("expected system prompt to include custom component %q", comp)
		}
	}

	indexBytes, err := fs.ReadFile(StaticFS, "dist/index.html")
	if err != nil {
		t.Fatalf("expected dist/index.html in StaticFS: %v", err)
	}
	if !strings.Contains(string(indexBytes), "<div id=\"root\"></div>") {
		t.Errorf("unexpected dist/index.html content: %s", string(indexBytes))
	}
}
