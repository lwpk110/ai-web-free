package accounts

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func makeTestJWT(email, accountID string, exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payloadMap := map[string]interface{}{
		"exp": exp,
		"https://api.openai.com/profile": map[string]interface{}{
			"email": email,
		},
		"https://api.openai.com/auth": map[string]interface{}{
			"chatgpt_account_id": accountID,
			"chatgpt_plan_type":  "free",
		},
	}
	payloadBytes, _ := json.Marshal(payloadMap)
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return header + "." + payload + ".sig1234567890"
}

func TestParseKamiText_AutoRecognizeWithATAndAnyOrder(t *testing.T) {
	at1 := makeTestJWT("u6@example.com", "acct-6", 4102444800)
	at2 := makeTestJWT("u2@example.com", "acct-2", 4102444800)
	at3 := makeTestJWT("u3@example.com", "acct-3", 4102444800)

	raw := `
# comment line
u1@example.com----Pass123!----JBSWY3DPEHPK3PXP
u2@example.com----` + at2 + `----Pass234!----MFRGGZDFMZTWQ2LK
` + at3 + `|u3@example.com|KRSXG5CTMVRXEZLU|Pass345!|rt_refresh_token_1234567890
Pass456!	u4@example.com	otpauth://totp/ChatGPT:u4@example.com?secret=NB2W45DFOIZA2345&issuer=OpenAI
u5@example.com----Pass567!
` + at1 + `----Pass678!----JBSWY3DPEHPK3PXP
`

	got := ParseKamiText(raw)
	if len(got) != 6 {
		t.Fatalf("expected 6 parsed kami accounts, got %d: %+v", len(got), got)
	}

	// 1. Standard 3-col
	if got[0].Email != "u1@example.com" || got[0].Password != "Pass123!" || got[0].TOTPSecret != "JBSWY3DPEHPK3PXP" || got[0].AccessToken != "" {
		t.Fatalf("unexpected row 0: %+v", got[0])
	}
	// 2. Email----AT----Pass----TOTP
	if got[1].Email != "u2@example.com" || got[1].Password != "Pass234!" || got[1].TOTPSecret != "MFRGGZDFMZTWQ2LK" || got[1].AccessToken != at2 {
		t.Fatalf("unexpected row 1: %+v", got[1])
	}
	// 3. AT|Email|TOTP|Pass|RT with pipe delimiter
	if got[2].Email != "u3@example.com" || got[2].Password != "Pass345!" || got[2].TOTPSecret != "KRSXG5CTMVRXEZLU" || got[2].AccessToken != at3 || got[2].RefreshToken != "rt_refresh_token_1234567890" {
		t.Fatalf("unexpected row 2: %+v", got[2])
	}
	// 4. Pass \t Email \t otpauth://...
	if got[3].Email != "u4@example.com" || got[3].Password != "Pass456!" || got[3].TOTPSecret != "NB2W45DFOIZA2345" {
		t.Fatalf("unexpected row 3: %+v", got[3])
	}
	// 5. Email----Pass (no 2FA)
	if got[4].Email != "u5@example.com" || got[4].Password != "Pass567!" || got[4].TOTPSecret != "" {
		t.Fatalf("unexpected row 4: %+v", got[4])
	}
	// 6. AT----Pass----TOTP (email extracted from JWT)
	if got[5].Email != "u6@example.com" {
		t.Fatalf("unexpected row 5 email: %+v", got[5])
	}
}

func TestParseKamiText_UsernamePasswordRTMode(t *testing.T) {
	raw := `
# Username + Password + Refresh Token (no rt_ prefix)
u7@example.com----Pass789!----a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6
u8@example.com|Pass890!|my-custom-oauth-refresh-token-xyz-123456
`
	got := ParseKamiText(raw)
	if len(got) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(got))
	}
	if got[0].Email != "u7@example.com" || got[0].Password != "Pass789!" || got[0].RefreshToken != "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6" {
		t.Fatalf("unexpected row 0: %+v", got[0])
	}
	if got[1].Email != "u8@example.com" || got[1].Password != "Pass890!" || got[1].RefreshToken != "my-custom-oauth-refresh-token-xyz-123456" {
		t.Fatalf("unexpected row 1: %+v", got[1])
	}
}
