package accounts

import (
	"strings"
	"testing"
)

func TestParseKamiText(t *testing.T) {
	raw := `
# comment line
alice@example.com----pass123!----OPWOLPW6KQMM6K4V674HAIE6YS2L56NR

bob@example.com----pass#456----5M22V32CLCU472EZKS27J7JWPWGZAUXV
alice@example.com----duplicate----OPWOLPW6KQMM6K4V674HAIE6YS2L56NR
invalid_line_only
`
	parsed := ParseKamiText(raw)
	if len(parsed) != 2 {
		t.Fatalf("expected 2 parsed accounts, got %d", len(parsed))
	}
	if parsed[0].Email != "alice@example.com" || parsed[0].Password != "pass123!" {
		t.Fatalf("unexpected first account: %+v", parsed[0])
	}
	if parsed[1].Email != "bob@example.com" || parsed[1].TOTPSecret != "5M22V32CLCU472EZKS27J7JWPWGZAUXV" {
		t.Fatalf("unexpected second account: %+v", parsed[1])
	}

	formatted := FormatKamiLines(parsed)
	if !strings.Contains(formatted, "alice@example.com----pass123!----OPWOLPW6KQMM6K4V674HAIE6YS2L56NR") {
		t.Fatalf("unexpected formatted output: %s", formatted)
	}
}

func TestParseSub2APIJSON(t *testing.T) {
	raw := `{
  "type": "sub2api-data",
  "version": 1,
  "accounts": [
    {
      "name": "user@example.com",
      "platform": "openai",
      "type": "oauth",
      "credentials": {
        "access_token": "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjMifQ.sig",
        "session_token": "sess-123",
        "chatgpt_account_id": "acct-123"
      },
      "extra": {
        "email": "user@example.com",
        "password": "pw",
        "totp_secret": "OPWOLPW6KQMM6K4V674HAIE6YS2L56NR"
      }
    }
  ]
}`
	data, err := ParseSub2APIJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data.Accounts) != 1 || data.Accounts[0].Credentials.SessionToken != "sess-123" {
		t.Fatalf("unexpected parsed data: %+v", data)
	}
}
