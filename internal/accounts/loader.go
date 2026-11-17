package accounts

import (
	"bufio"
	"fmt"
	"hash/fnv"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// RawToken 从文件加载的原始 token 信息
type RawToken struct {
	Token     string // access_token / refresh_token / session_token
	TeamID    string // 冒号后（仅 access_tokens.txt 支持）
}

// LoadTokensFromFile 从文件读取 token，空行和 # 开头被忽略
func LoadTokensFromFile(path string) []RawToken {
	f, err := os.Open(path)
	if err != nil {
		return []RawToken{}
	}
	defer f.Close()

	var tokens []RawToken
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		token := strings.TrimSpace(parts[0])
		if token == "" {
			continue
		}
		t := RawToken{Token: token}
		if len(parts) > 1 {
			t.TeamID = strings.TrimSpace(parts[1])
		}
		tokens = append(tokens, t)
	}
	return tokens
}

// LoadBannedFromFile 从文件加载已封禁账号映射表 (key: lowercase identifier, val: reason)
func LoadBannedFromFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer f.Close()

	banned := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "----", 2)
		if len(parts) == 1 {
			parts = strings.SplitN(line, ":", 2)
		}
		id := strings.ToLower(strings.TrimSpace(parts[0]))
		reason := "账号已被封禁/停用"
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			reason = strings.TrimSpace(parts[1])
		}
		if id != "" {
			banned[id] = reason
		}
	}
	return banned
}

// AppendBannedToFile 将封禁账号追加写入持久化文件（避免重复写入）
func AppendBannedToFile(path, identifier, reason string) error {
	id := strings.ToLower(strings.TrimSpace(identifier))
	if id == "" {
		return nil
	}
	existing := LoadBannedFromFile(path)
	if _, ok := existing[id]; ok {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	cleanReason := strings.ReplaceAll(reason, "\n", " ")
	_, err = f.WriteString(fmt.Sprintf("%s----%s\n", id, cleanReason))
	return err
}

// RemoveBannedFromFile 从封禁文件中移除解封或重新激活的账号
func RemoveBannedFromFile(path, identifier string) error {
	id := strings.ToLower(strings.TrimSpace(identifier))
	if id == "" {
		return nil
	}
	existing := LoadBannedFromFile(path)
	if _, ok := existing[id]; !ok {
		return nil
	}
	delete(existing, id)

	var sb strings.Builder
	for k, v := range existing {
		sb.WriteString(fmt.Sprintf("%s----%s\n", k, v))
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// LoadDisabledFromFile 从文件加载已禁用账号名单 (key: lowercase identifier, val: reason)
func LoadDisabledFromFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer f.Close()

	disabled := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "----", 2)
		if len(parts) == 1 {
			parts = strings.SplitN(line, ":", 2)
		}
		id := strings.ToLower(strings.TrimSpace(parts[0]))
		reason := "管理员手动禁用"
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			reason = strings.TrimSpace(parts[1])
		}
		if id != "" {
			disabled[id] = reason
		}
	}
	return disabled
}

// AppendDisabledToFile 追加被手动禁用的账号到文件
func AppendDisabledToFile(path, identifier, reason string) error {
	id := strings.ToLower(strings.TrimSpace(identifier))
	if id == "" {
		return nil
	}
	existing := LoadDisabledFromFile(path)
	if _, ok := existing[id]; ok {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	cleanReason := strings.ReplaceAll(reason, "\n", " ")
	_, err = f.WriteString(fmt.Sprintf("%s----%s\n", id, cleanReason))
	return err
}

// RemoveDisabledFromFile 从手动禁用名单中移除已启用的账号
func RemoveDisabledFromFile(path, identifier string) error {
	id := strings.ToLower(strings.TrimSpace(identifier))
	if id == "" {
		return nil
	}
	existing := LoadDisabledFromFile(path)
	if _, ok := existing[id]; !ok {
		return nil
	}
	delete(existing, id)

	var sb strings.Builder
	for k, v := range existing {
		sb.WriteString(fmt.Sprintf("%s----%s\n", k, v))
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// LoadAccountProxiesFromFile 从文件加载账号粘性代理绑定关系 (key: lowercase identifier, val: proxy_url)
func LoadAccountProxiesFromFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer f.Close()

	bindings := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "----", 2)
		if len(parts) < 2 {
			continue
		}
		id := strings.ToLower(strings.TrimSpace(parts[0]))
		proxyURL := strings.TrimSpace(parts[1])
		if id != "" && proxyURL != "" {
			bindings[id] = proxyURL
		}
	}
	return bindings
}

// SaveAccountProxiesToFile 将全量账号代理粘性绑定写回文件
func SaveAccountProxiesToFile(path string, bindings map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	var sb strings.Builder
	for k, v := range bindings {
		if k != "" && v != "" {
			sb.WriteString(fmt.Sprintf("%s----%s\n", k, v))
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// SetAccountProxyInFile 设置单条账号代理绑定持久化记录
func SetAccountProxyInFile(path, identifier, proxyURL string) error {
	id := strings.ToLower(strings.TrimSpace(identifier))
	if id == "" {
		return nil
	}
	bindings := LoadAccountProxiesFromFile(path)
	if proxyURL == "" {
		delete(bindings, id)
	} else {
		bindings[id] = strings.TrimSpace(proxyURL)
	}
	return SaveAccountProxiesToFile(path, bindings)
}

// CreateAccount 从原始 token 创建 Account，分配粘性画像指纹但不初始化 Client
func CreateAccount(token string, acctType AccountType, profilePool []FingerprintProfile) *Account {
	acct := NewAccount(uuid.NewString(), acctType, token)
	seedKey := acct.Email
	if seedKey == "" {
		seedKey = acct.ChatGPTAccountID
	}
	if seedKey == "" {
		seedKey = token
	}
	acct.Fingerprint = deterministicProfile(seedKey, profilePool)
	return acct
}

// deterministicProfile 基于种子 key 确定性选取指纹画像，保证重启后 TLS 指纹与 UA 粘性一致
func deterministicProfile(seedKey string, profiles []FingerprintProfile) BrowserFingerprint {
	if len(profiles) == 0 {
		return BrowserFingerprint{
			OaiDeviceID:  uuid.NewString(),
			OaiSessionID: uuid.NewString(),
		}
	}
	idx := 0
	if seedKey != "" {
		h := fnv.New32a()
		_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(seedKey))))
		idx = int(h.Sum32()) % len(profiles)
	} else {
		idx = rand.Intn(len(profiles))
	}
	p := profiles[idx]
	return BrowserFingerprint{
		OaiDeviceID:         uuid.NewString(),
		OaiSessionID:        uuid.NewString(),
		UserAgent:           p.UserAgent,
		ScreenWidth:         p.ScreenWidth,
		ScreenHeight:        p.ScreenHeight,
		HardwareConcurrency: p.HardwareConcurrency,
		Platform:            p.Platform,
		TLSProfileName:      p.TLSProfileName,
	}
}

// randomProfile 从画像池随机选一个
func randomProfile(profiles []FingerprintProfile) BrowserFingerprint {
	return deterministicProfile("", profiles)
}
