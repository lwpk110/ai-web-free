package accounts

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// KamiAccount 表示单条标准卡密账号凭据（邮箱----密码----2FA密钥）。
type KamiAccount struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totp_secret"`
	LineNo     int    `json:"line_no,omitempty"`
}

// ParseKamiText 解析多行卡密文本，支持 `----` 分隔或空白 3 列格式，自动过滤空行与 # 注释。
func ParseKamiText(raw string) []KamiAccount {
	var out []KamiAccount
	seen := make(map[string]bool)
	lines := strings.Split(raw, "\n")
	for idx, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		var email, password, totpSecret string
		if strings.Contains(line, "----") {
			parts := strings.Split(line, "----")
			if len(parts) != 3 {
				continue
			}
			email = strings.TrimSpace(parts[0])
			password = strings.TrimSpace(parts[1])
			totpSecret = strings.TrimSpace(parts[2])
		} else {
			fields := strings.Fields(line)
			if len(fields) != 3 {
				continue
			}
			email = strings.TrimSpace(fields[0])
			password = strings.TrimSpace(fields[1])
			totpSecret = strings.TrimSpace(fields[2])
		}

		if email == "" || password == "" || totpSecret == "" {
			continue
		}
		lowerEmail := strings.ToLower(email)
		if seen[lowerEmail] {
			continue
		}
		seen[lowerEmail] = true
		out = append(out, KamiAccount{
			Email:      email,
			Password:   password,
			TOTPSecret: totpSecret,
			LineNo:     idx + 1,
		})
	}
	return out
}

// LoadKamiFromFile 从本地卡密文件加载账号凭据列表。
func LoadKamiFromFile(path string) []KamiAccount {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return ParseKamiText(string(data))
}

// FormatKamiLines 将卡密账号列表格式化为标准单行文本。
func FormatKamiLines(accounts []KamiAccount) string {
	var b strings.Builder
	for _, acc := range accounts {
		if acc.Email == "" || acc.Password == "" || acc.TOTPSecret == "" {
			continue
		}
		b.WriteString(acc.Email)
		b.WriteString("----")
		b.WriteString(acc.Password)
		b.WriteString("----")
		b.WriteString(acc.TOTPSecret)
		b.WriteByte('\n')
	}
	return b.String()
}

// Sub2APIData 兼容 Sub2API 导出的 JSON 数据包格式（*_sub2api.json）。
type Sub2APIData struct {
	Type       string           `json:"type"`
	Version    int              `json:"version"`
	ExportedAt string           `json:"exported_at"`
	Accounts   []Sub2APIAccount `json:"accounts"`
}

type Sub2APIAccount struct {
	Name        string             `json:"name"`
	Platform    string             `json:"platform"`
	Type        string             `json:"type"`
	Concurrency int                `json:"concurrency"`
	Priority    int                `json:"priority"`
	Credentials Sub2APICredentials `json:"credentials"`
	Extra       Sub2APIExtra       `json:"extra"`
}

type Sub2APICredentials struct {
	RefreshToken     string `json:"refresh_token"`
	AccessToken      string `json:"access_token"`
	IDToken          string `json:"id_token"`
	SessionToken     string `json:"session_token,omitempty"`
	ChatGPTAccountID string `json:"chatgpt_account_id,omitempty"`
	ExpiresAt        int64  `json:"expires_at"`
}

type Sub2APIExtra struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totp_secret"`
}

// ParseSub2APIJSON 解析 Sub2API JSON 数据包。
func ParseSub2APIJSON(raw string) (*Sub2APIData, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty sub2api json")
	}
	var data Sub2APIData
	if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
		return nil, fmt.Errorf("invalid sub2api json: %w", err)
	}
	if len(data.Accounts) == 0 {
		return nil, fmt.Errorf("sub2api json contains no accounts")
	}
	return &data, nil
}
