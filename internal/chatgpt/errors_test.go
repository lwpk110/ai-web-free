package chatgpt

import (
	"testing"
)

func TestLooksLikeAccountBannedDeactivated(t *testing.T) {
	cases := []struct {
		statusCode int
		msg        string
		expected   bool
	}{
		{
			statusCode: 400,
			msg:        "你没有账户，因为该账户已被删除或停用。如果你认为这是错误，请通过我们的帮助中心 help.openai.com 联系我们。error_code: account_deactivated",
			expected:   true,
		},
		{
			statusCode: 0,
			msg:        "account_deactivated: 该账户已被删除或停用 (error_code: account_deactivated)",
			expected:   true,
		},
		{
			statusCode: 403,
			msg:        "Your OpenAI account has been deactivated.",
			expected:   true,
		},
		{
			statusCode: 403,
			msg:        "Cloudflare WAF: Checking your browser before accessing chatgpt.com",
			expected:   false,
		},
		{
			statusCode: 429,
			msg:        "Rate limit reached for default-gpt-4o",
			expected:   false,
		},
		{
			statusCode: 401,
			msg:        "token_expired",
			expected:   false,
		},
	}

	for _, tc := range cases {
		actual := LooksLikeAccountBanned(tc.statusCode, tc.msg)
		if actual != tc.expected {
			t.Errorf("LooksLikeAccountBanned(%d, %q) = %v; want %v", tc.statusCode, tc.msg, actual, tc.expected)
		}
	}
}
