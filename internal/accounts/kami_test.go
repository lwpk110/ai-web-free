package accounts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildSub2APIJSONSecretRedaction(t *testing.T) {
	acct := CreateAccount("eyJhbGciOiJIUzI1NiJ9.payload.sig", TypeFree, DefaultProfiles)
	acct.Email = "user@example.com"
	acct.Password = "super-secret-pass"
	acct.TOTPSecret = "JBSWY3DPEHPK3PXP"
	acct.ChatGPTAccountID = "org-123"
	acct.ExpiresAt = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	masked := BuildSub2APIJSON([]*Account{acct}, false)
	rawMasked, err := json.Marshal(masked)
	if err != nil {
		t.Fatalf("marshal masked failed: %v", err)
	}
	if strings.Contains(string(rawMasked), "super-secret-pass") || strings.Contains(string(rawMasked), "JBSWY3DPEHPK3PXP") {
		t.Fatalf("expected secrets redacted when includeSecrets=false: %s", string(rawMasked))
	}
	parsedMasked, err := ParseSub2APIJSON(string(rawMasked))
	if err != nil || len(parsedMasked.Accounts) != 1 || parsedMasked.Accounts[0].Credentials.AccessToken != acct.Token {
		t.Fatalf("roundtrip failed: %+v err=%v", parsedMasked, err)
	}

	full := BuildSub2APIJSON([]*Account{acct}, true)
	rawFull, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal full failed: %v", err)
	}
	if !strings.Contains(string(rawFull), "super-secret-pass") || !strings.Contains(string(rawFull), "JBSWY3DPEHPK3PXP") {
		t.Fatalf("expected secrets present when includeSecrets=true: %s", string(rawFull))
	}
}
