package provider

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseGrokCardLine(t *testing.T) {
	card, err := ParseGrokCardLine("user@example.com|pass-word|sso=old; sso-rw=old-rw", 3)
	if err != nil {
		t.Fatal(err)
	}
	if card.Email != "user@example.com" || card.Password != "pass-word" || card.TOTPSecret != "" {
		t.Fatalf("unexpected card: %+v", card)
	}
	if card.OldCookie != "sso=old; sso-rw=old-rw" {
		t.Fatalf("unexpected old cookie: %q", card.OldCookie)
	}
}

func TestDetectGrokCredentialLine(t *testing.T) {
	cases := []struct {
		line string
		kind string
	}{
		{"user@example.com|password|JBSWY3DPEHPK3PXP", "kami"},
		{"user@example.com----password----JBSWY3DPEHPK3PXP", "kami"},
		{"user@example.com----sso=old; sso-rw=old-rw", "cookie"},
		{"sso=pure; sso-rw=pure-rw", "cookie"},
	}
	for _, tc := range cases {
		kind, card, err := DetectGrokCredentialLine(tc.line)
		if err != nil {
			t.Fatalf("detect %q failed: %v", tc.line, err)
		}
		if kind != tc.kind {
			t.Fatalf("detect %q kind = %q, want %q", tc.line, kind, tc.kind)
		}
		if kind == "kami" && card.Email == "" {
			t.Fatalf("detect %q did not return card data", tc.line)
		}
	}
	if _, _, err := DetectGrokCredentialLine("not-a-grok-credential"); err == nil {
		t.Fatal("expected unsupported credential to fail")
	}
}

func TestParseGrokCardLineWithProxy(t *testing.T) {
	card, err := ParseGrokCardLine("user@example.com----pass-word----JBSWY3DPEHPK3PXP----http://127.0.0.1:7890", 1)
	if err != nil {
		t.Fatal(err)
	}
	if card.TOTPSecret != "JBSWY3DPEHPK3PXP" || card.Proxy != "http://127.0.0.1:7890" || card.OldCookie != "" {
		t.Fatalf("unexpected card: %+v", card)
	}
}

func TestParseGrokCardLineWithLegacyJWT(t *testing.T) {
	token := "eyJhbGciOiJIUzI1NiJ9.eyJzZXNzaW9uX2lkIjoiMTIzNDU2Nzg5MCJ9.c2lnbmF0dXJl"
	card, err := ParseGrokCardLine("user@example.com|password123|"+token, 1)
	if err != nil {
		t.Fatal(err)
	}
	expectedCookie := "sso=" + token + "; sso-rw=" + token
	if card.LegacyToken != token || card.TOTPSecret != "" || card.OldCookie != expectedCookie {
		t.Fatalf("unexpected legacy JWT card: %+v", card)
	}
}

func TestParseGrokCardLineRejectsInvalid(t *testing.T) {
	if _, err := ParseGrokCardLine("user@example.com|password", 7); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParseGrokCardLine("user@example.com|password|short", 8); err == nil {
		t.Fatal("expected invalid TOTP error")
	}
}

func TestParseGrokCardFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cards.txt")
	content := "# comment\n\na@example.com|password1|JBSWY3DPEHPK3PXP\nb@example.com----password2----JBSWY3DPEHPK3PXP\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cards, err := ParseGrokCardFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].LineNo != 3 || cards[1].Email != "b@example.com" {
		t.Fatalf("unexpected cards: %+v", cards)
	}
}

func TestParseGrokCardFileWithHeadersAndLegacyJWT(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cards_with_header.txt")
	content := `=== 使用说明 ===
批量提取工具： https://example.com 密码:xyz

=== 卡密内容 ===
user1@example.com|pass1|eyJhbGciOiJIUzI1NiJ9.eyJzZXNzaW9uX2lkIjoiMTIzIn0.X2E9cw5QUAA6QspOIz7qOPEB3b12i0stW91+bA==
user2@example.com----pass2----JBSWY3DPEHPK3PXP
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cards, err := ParseGrokCardFile(path)
	if err != nil {
		t.Fatalf("expected file to parse cleanly with headers: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
	if cards[0].Email != "user1@example.com" || cards[0].LegacyToken == "" || cards[0].TOTPSecret != "" {
		t.Fatalf("unexpected card 0: %+v", cards[0])
	}
	if cards[1].Email != "user2@example.com" || cards[1].TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("unexpected card 1: %+v", cards[1])
	}
}

func TestFilterGrokCardAccounts(t *testing.T) {
	cards := []GrokCardAccount{
		{Email: "alpha@example.com"},
		{Email: "beta@example.com"},
		{Email: "gamma@example.com"},
	}
	if filtered := FilterGrokCardAccounts(cards, "missing", 0); len(filtered) != 0 {
		t.Fatalf("expected no matches, got %+v", filtered)
	}
	if filtered := FilterGrokCardAccounts(cards, "", 2); len(filtered) != 2 || filtered[1].Email != "beta@example.com" {
		t.Fatalf("unexpected filtered cards: %+v", filtered)
	}
}

func TestFormatGrokSSOCookie(t *testing.T) {
	cookie := FormatGrokSSOCookie(map[string]string{"sso-rw": "rw", "sso": "ro"})
	if cookie != "sso=ro; sso-rw=rw" {
		t.Fatalf("unexpected cookie: %q", cookie)
	}
}

func TestGrokCookieDomainAllowed(t *testing.T) {
	for _, domain := range []string{"grok.com", ".grok.com", "x.ai", "accounts.x.ai"} {
		if !grokCookieDomainAllowed(domain) {
			t.Fatalf("expected domain %q to be allowed", domain)
		}
	}
	if grokCookieDomainAllowed("example.com") {
		t.Fatal("expected unrelated domain to be rejected")
	}
}

func TestWriteGrokCookieOutputAndMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sso.txt")
	if err := os.WriteFile(path, []byte("old@example.com----sso=old\n"), 0600); err != nil {
		t.Fatal(err)
	}

	results := []GrokLoginResult{
		{Email: "old@example.com", Error: "expired"},
		{Email: "new@example.com", OK: true, Cookie: "sso=fresh"},
	}
	written, err := WriteGrokCookieOutput(path, results, true)
	if err != nil {
		t.Fatal(err)
	}
	if written != 2 {
		t.Fatalf("expected 2 entries, got %d", written)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "new@example.com----sso=fresh" || lines[1] != "old@example.com----sso=old" {
		t.Fatalf("unexpected output: %q", lines)
	}
}

func TestWriteGrokCookieOutputCreatesParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "sso.txt")
	results := []GrokLoginResult{{Email: "user@example.com", OK: true, Cookie: "sso=value"}}
	written, err := WriteGrokCookieOutput(path, results, false)
	if err != nil || written != 1 {
		t.Fatalf("write=%d err=%v", written, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected file mode: %v", info.Mode().Perm())
	}
}
