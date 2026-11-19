package accounts

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// KamiAccount 表示单条卡密账号凭据（支持邮箱、密码、2FA 密钥、可选 AccessToken / RefreshToken）。
type KamiAccount struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	TOTPSecret   string `json:"totp_secret"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	LineNo       int    `json:"line_no,omitempty"`
}

// ParseKamiText 解析多行卡密文本，支持 `----` / `|` / `\t` / `;` / 空白分隔，
// 自动识别邮箱、密码、2FA 密钥、Access Token (JWT) 与 Refresh Token (rt_*)，不限字段顺序，自动过滤空行与 # 注释。
func ParseKamiText(raw string) []KamiAccount {
	var out []KamiAccount
	seen := make(map[string]bool)
	lines := strings.Split(raw, "\n")
	for idx, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := splitKamiFields(line)
		if len(fields) < 2 {
			continue
		}

		acc, ok := classifyKamiFields(fields)
		if !ok {
			continue
		}

		lowerEmail := strings.ToLower(acc.Email)
		if seen[lowerEmail] {
			continue
		}
		seen[lowerEmail] = true
		acc.LineNo = idx + 1
		out = append(out, acc)
	}
	return out
}

func splitKamiFields(line string) []string {
	var rawParts []string
	switch {
	case strings.Contains(line, "----"):
		rawParts = strings.Split(line, "----")
	case strings.Contains(line, "|"):
		rawParts = strings.Split(line, "|")
	case strings.Contains(line, "\t"):
		rawParts = strings.Split(line, "\t")
	case strings.Contains(line, ";"):
		rawParts = strings.Split(line, ";")
	default:
		rawParts = strings.Fields(line)
	}
	out := make([]string, 0, len(rawParts))
	for _, p := range rawParts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func classifyKamiFields(fields []string) (KamiAccount, bool) {
	var acc KamiAccount
	var leftovers []string

	for _, f := range fields {
		switch {
		case isLikelyJWT(f):
			// 若出现多段 JWT（如 id_token + access_token），优先保留含 chatgpt_account_id 的令牌
			if acc.AccessToken == "" || (ExtractChatGPTAccountID(acc.AccessToken) == "" && ExtractChatGPTAccountID(f) != "") {
				acc.AccessToken = f
			}
		case strings.HasPrefix(f, "rt_") && len(f) >= 16:
			if acc.RefreshToken == "" {
				acc.RefreshToken = f
			}
		case strings.HasPrefix(strings.ToLower(f), "otpauth://"):
			if secret := extractOTPSecretFromURI(f); secret != "" && acc.TOTPSecret == "" {
				acc.TOTPSecret = secret
			}
		case isLikelyEmail(f):
			if acc.Email == "" {
				acc.Email = f
			} else {
				leftovers = append(leftovers, f)
			}
		default:
			leftovers = append(leftovers, f)
		}
	}

	if acc.Email == "" && acc.AccessToken != "" {
		acc.Email = ExtractEmail(acc.AccessToken)
	}
	if acc.Email == "" {
		return KamiAccount{}, false
	}

	// 优先识别严格大写 Base32 2FA 密钥（避免误判纯小写密码）
	if acc.TOTPSecret == "" {
		var nextLeftovers []string
		for _, f := range leftovers {
			if acc.TOTPSecret == "" && isLikelyTOTPSecret(f, true) {
				acc.TOTPSecret = normalizeTOTPSecret(f)
				continue
			}
			nextLeftovers = append(nextLeftovers, f)
		}
		leftovers = nextLeftovers
	}

	// 若仍未识别到 2FA，且剩余字段不少于 2 个（或已带 AccessToken），尝试大小写不敏感 Base32 识别
	if acc.TOTPSecret == "" && (len(leftovers) >= 2 || (acc.AccessToken != "" && len(leftovers) == 1)) {
		var nextLeftovers []string
		for _, f := range leftovers {
			if acc.TOTPSecret == "" && isLikelyTOTPSecret(f, false) {
				acc.TOTPSecret = normalizeTOTPSecret(f)
				continue
			}
			nextLeftovers = append(nextLeftovers, f)
		}
		leftovers = nextLeftovers
	}

	if len(leftovers) > 0 {
		acc.Password = leftovers[0]
	}
	if len(leftovers) > 1 && acc.RefreshToken == "" {
		for _, rem := range leftovers[1:] {
			if len(rem) >= 20 && !strings.ContainsAny(rem, " \t\r\n") {
				acc.RefreshToken = rem
				break
			}
		}
	}

	if acc.Password == "" && acc.AccessToken == "" {
		return KamiAccount{}, false
	}
	return acc, true
}

func isLikelyEmail(s string) bool {
	if strings.ContainsAny(s, " \t\r\n") || strings.HasPrefix(s, "eyJ") {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at <= 0 || at >= len(s)-3 {
		return false
	}
	domain := s[at+1:]
	if strings.Contains(domain, "@") {
		return false
	}
	dot := strings.LastIndexByte(domain, '.')
	return dot > 0 && dot < len(domain)-1
}

func isLikelyJWT(s string) bool {
	if !strings.HasPrefix(s, "eyJ") || len(s) < 40 || strings.Count(s, ".") != 2 {
		return false
	}
	_, err := ParseJWTClaims(s)
	return err == nil
}

func extractOTPSecretFromURI(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	sec := strings.TrimSpace(u.Query().Get("secret"))
	if isLikelyTOTPSecret(sec, false) {
		return normalizeTOTPSecret(sec)
	}
	return ""
}

func normalizeTOTPSecret(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	return strings.ToUpper(strings.TrimSpace(s))
}

func isLikelyTOTPSecret(s string, strictUpper bool) bool {
	cleaned := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), " ", ""), "-", "")
	if len(cleaned) < 16 || len(cleaned) > 64 {
		return false
	}
	if strictUpper && cleaned != strings.ToUpper(cleaned) {
		return false
	}
	upper := strings.ToUpper(cleaned)
	hasLetter := false
	seenPad := false
	for i := 0; i < len(upper); i++ {
		c := upper[i]
		if c == '=' {
			seenPad = true
			continue
		}
		if seenPad {
			return false
		}
		if c >= 'A' && c <= 'Z' {
			hasLetter = true
			continue
		}
		if c >= '2' && c <= '7' {
			continue
		}
		return false
	}
	return hasLetter
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

// BuildSub2APIJSON 从账号列表生成 Sub2API 导入包；includeSecrets 为 false 时自动脱敏明文密码与 2FA 密钥。
func BuildSub2APIJSON(accts []*Account, includeSecrets bool) Sub2APIData {
	data := Sub2APIData{
		Type:       "sub2api-data",
		Version:    1,
		ExportedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Accounts:   make([]Sub2APIAccount, 0, len(accts)),
	}
	for _, a := range accts {
		if a == nil || a.IsTemporary {
			continue
		}
		name := strings.TrimSpace(a.Email)
		if name == "" {
			name = a.ID
		}
		var expUnix int64
		if !a.ExpiresAt.IsZero() {
			expUnix = a.ExpiresAt.Unix()
		}
		extra := Sub2APIExtra{
			Email: a.Email,
		}
		if includeSecrets {
			extra.Password = a.Password
			extra.TOTPSecret = a.TOTPSecret
		}
		data.Accounts = append(data.Accounts, Sub2APIAccount{
			Name:        name,
			Platform:    "openai",
			Type:        "oauth",
			Concurrency: 10,
			Priority:    1,
			Credentials: Sub2APICredentials{
				RefreshToken:     a.RefreshToken,
				AccessToken:      a.Token,
				IDToken:          "",
				SessionToken:     a.SessionToken,
				ChatGPTAccountID: a.ChatGPTAccountID,
				ExpiresAt:        expUnix,
			},
			Extra: extra,
		})
	}
	return data
}
