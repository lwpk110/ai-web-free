package provider

import (
	"context"
	"strings"
	"testing"

	"ai-web-free/internal/accounts"
	appdb "ai-web-free/internal/db"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openGrokPatternDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:?_pragma=busy_timeout(5000)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open pattern db: %v", err)
	}
	if err := database.AutoMigrate(&appdb.CredentialFormatPatternRecord{}); err != nil {
		t.Fatalf("migrate pattern db: %v", err)
	}
	return database
}

func TestGrokCredentialPatternLearningAndOfflineReuse(t *testing.T) {
	database := openGrokPatternDB(t)
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	p.SetGrokCredentialPatternDB(database)
	p.grokCredentialLLMFn = func(_ context.Context, lines []string) ([]grokCredentialLLMResult, error) {
		if len(lines) != 1 || !strings.Contains(lines[0], "first@example.com") {
			t.Fatalf("unexpected first LLM input: %d line(s)", len(lines))
		}
		return []grokCredentialLLMResult{{
			Index: 0,
			Kind:  "kami",
			Value: "first@example.com|first-password|JBSWY3DPEHPK3PXP",
		}}, nil
	}

	first, err := p.ImportCredentials(ImportRequest{
		CredentialType: "auto",
		RawText:        "邮箱: first@example.com 密码: first-password 2FA: JBSW Y3DP EHPK 3PXP",
		Persist:        false,
	})
	if err != nil {
		t.Fatalf("first import failed: %v", err)
	}
	if first.Added != 1 || first.Failed != 0 {
		t.Fatalf("unexpected first import: %+v", first)
	}

	var records []appdb.CredentialFormatPatternRecord
	if err := database.Find(&records).Error; err != nil || len(records) != 1 {
		t.Fatalf("expected one learned pattern, records=%d err=%v", len(records), err)
	}
	record := records[0]
	for _, secret := range []string{"first@example.com", "first-password", "JBSWY3DPEHPK3PXP"} {
		if strings.Contains(record.InputTemplate, secret) || strings.Contains(record.OutputTemplate, secret) {
			t.Fatalf("pattern stores credential %q: input=%q output=%q", secret, record.InputTemplate, record.OutputTemplate)
		}
	}

	p.grokCredentialLLMFn = func(context.Context, []string) ([]grokCredentialLLMResult, error) {
		t.Fatal("learned format should not call the LLM again")
		return nil, nil
	}
	second, err := p.ImportCredentials(ImportRequest{
		CredentialType: "auto",
		RawText:        "邮箱: second@example.com 密码: second-password 2FA: JBSW Y3DP EHPK 3PXP",
		Persist:        false,
	})
	if err != nil {
		t.Fatalf("second import failed: %v", err)
	}
	if second.Added != 1 || second.Failed != 0 {
		t.Fatalf("unexpected second import: %+v", second)
	}
	if len(second.Messages) != 2 || !strings.Contains(second.Messages[1], "记忆格式：卡密 1 条") {
		t.Fatalf("unexpected second import messages: %+v", second.Messages)
	}

	if err := database.First(&record, "id = ?", record.ID).Error; err != nil {
		t.Fatalf("reload pattern: %v", err)
	}
	if record.UseCount != 1 || record.SuccessCount != 1 || record.FailureCount != 0 || !record.Enabled {
		t.Fatalf("pattern usage counters were not updated: %+v", record)
	}
}

func TestGrokCredentialPatternIsDisabledAfterRepeatedBadMatches(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	pattern := appdb.CredentialFormatPatternRecord{
		ID:             "pattern-too-broad",
		Provider:       "grok",
		CredentialType: "kami",
		InputTemplate:  "邮箱: {email} 密码: {password} 2FA: {totp}",
		OutputTemplate: "{email}|{password}|{totp}",
		Enabled:        true,
	}
	database := openGrokPatternDB(t)
	if err := database.Create(&pattern).Error; err != nil {
		t.Fatalf("seed pattern: %v", err)
	}
	p.SetGrokCredentialPatternDB(database)
	for i := 0; i < 3; i++ {
		if _, _, ok := p.matchLearnedGrokCredential("邮箱: bad 密码: values 2FA: shape", []appdb.CredentialFormatPatternRecord{pattern}); ok {
			t.Fatalf("bad line unexpectedly matched through provider")
		}
	}
	var updated appdb.CredentialFormatPatternRecord
	if err := database.First(&updated, "id = ?", pattern.ID).Error; err != nil {
		t.Fatalf("reload pattern: %v", err)
	}
	if updated.Enabled || updated.FailureCount != 3 {
		t.Fatalf("bad pattern was not disabled: %+v", updated)
	}
}

func TestGrokCredentialPatternRejectsUnrecognizedSecretRemnant(t *testing.T) {
	_, err := buildGrokCredentialPatternRecord(
		"邮箱: user@example.com 密码: password123 2FA: JBSWY3DPEHPK3PXP note: ABCDEFGHIJKLMNOPQRSTUV",
		"user@example.com|password123|JBSWY3DPEHPK3PXP",
		"kami",
	)
	if err == nil {
		t.Fatal("expected template with an unrecognized secret remnant to be rejected")
	}
}
