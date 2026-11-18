package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/db"
)

// SyncLocalFilesToDB 扫描本地凭据文件并增量写入 SQLite 数据库（用于备份前对齐）
func SyncLocalFilesToDB(cfg *config.Config) (int, error) {
	if cfg == nil {
		loaded := config.Load()
		cfg = &loaded
	}
	database, err := db.InitDB(cfg.DatabaseURL)
	if err != nil {
		return 0, fmt.Errorf("打开数据库失败: %w", err)
	}

	profiles := accounts.DefaultProfiles
	accs, err := accounts.LoadAccountsFromDB(database, profiles)
	if err != nil {
		accs = []*accounts.Account{}
	}

	findExisting := func(email, chatGPTID, token string) *accounts.Account {
		emailClean := strings.ToLower(strings.TrimSpace(email))
		idClean := strings.TrimSpace(chatGPTID)
		tokenClean := strings.TrimSpace(token)
		for _, a := range accs {
			if emailClean != "" && strings.ToLower(strings.TrimSpace(a.Email)) == emailClean {
				return a
			}
			if idClean != "" && strings.TrimSpace(a.ChatGPTAccountID) == idClean {
				return a
			}
			if tokenClean != "" && (a.Token == tokenClean || a.RefreshToken == tokenClean || a.SessionToken == tokenClean) {
				return a
			}
		}
		return nil
	}

	// 1. 合并 access_tokens.txt
	for _, t := range accounts.LoadTokensFromFile("access_tokens.txt") {
		chatGPTID := accounts.ExtractChatGPTAccountID(t.Token)
		email := accounts.ExtractEmail(t.Token)
		if matched := findExisting(email, chatGPTID, t.Token); matched != nil {
			if matched.Token == "" {
				matched.Token = t.Token
			}
			if matched.TeamUserID == "" {
				matched.TeamUserID = t.TeamID
			}
			if matched.ChatGPTAccountID == "" {
				matched.ChatGPTAccountID = chatGPTID
			}
			if matched.Email == "" {
				matched.Email = email
			}
			continue
		}
		acct := accounts.CreateAccount(t.Token, accounts.TypeFree, profiles)
		acct.TeamUserID = t.TeamID
		acct.ChatGPTAccountID = chatGPTID
		if email != "" {
			acct.Email = email
		}
		acct.Status = accounts.StatusActive
		accs = append(accs, acct)
	}

	// 2. 合并 refresh_tokens.txt
	for _, t := range accounts.LoadTokensFromFile("refresh_tokens.txt") {
		if matched := findExisting("", "", t.Token); matched != nil {
			if matched.RefreshToken == "" {
				matched.RefreshToken = t.Token
			}
			continue
		}
		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.RefreshToken = t.Token
		acct.TeamUserID = t.TeamID
		acct.Status = accounts.StatusExpired
		accs = append(accs, acct)
	}

	// 3. 合并 session_tokens.txt
	for _, t := range accounts.LoadTokensFromFile("session_tokens.txt") {
		if matched := findExisting("", "", t.Token); matched != nil {
			if matched.SessionToken == "" {
				matched.SessionToken = t.Token
			}
			continue
		}
		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.SessionToken = t.Token
		acct.TeamUserID = t.TeamID
		acct.Status = accounts.StatusExpired
		accs = append(accs, acct)
	}

	// 4. 合并 free_tokens.txt
	for _, t := range accounts.LoadTokensFromFile("free_tokens.txt") {
		if matched := findExisting("", "", t.Token); matched != nil {
			continue
		}
		acct := accounts.CreateAccount(t.Token, accounts.TypeNoAuth, profiles)
		acct.Status = accounts.StatusActive
		accs = append(accs, acct)
	}

	// 5. 合并 kami_accounts.txt
	for _, k := range accounts.LoadKamiFromFile("kami_accounts.txt") {
		if matched := findExisting(k.Email, "", ""); matched != nil {
			matched.Password = k.Password
			matched.TOTPSecret = k.TOTPSecret
			continue
		}
		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.Email = k.Email
		acct.Password = k.Password
		acct.TOTPSecret = k.TOTPSecret
		acct.Status = accounts.StatusPending
		acct.LastError = "待浏览器登录提取 Token"
		accs = append(accs, acct)
	}

	if err := accounts.BatchSaveAccountsToDB(database, accs); err != nil {
		return len(accs), fmt.Errorf("批量保存至数据库失败: %w", err)
	}
	return len(accs), nil
}

// ExportDBToLocalFiles 从 SQLite 数据库读取账号并回写至本地文本文件（用于恢复后对齐）
func ExportDBToLocalFiles(cfg *config.Config) (int, error) {
	if cfg == nil {
		loaded := config.Load()
		cfg = &loaded
	}
	database, err := db.InitDB(cfg.DatabaseURL)
	if err != nil {
		return 0, fmt.Errorf("打开数据库失败: %w", err)
	}

	profiles := accounts.DefaultProfiles
	accs, err := accounts.LoadAccountsFromDB(database, profiles)
	if err != nil {
		return 0, err
	}
	if len(accs) == 0 {
		return 0, nil
	}

	// 导出 access_tokens.txt
	var accessLines []string
	for _, a := range accs {
		if a.Type != accounts.TypeNoAuth && a.Token != "" {
			line := a.Token
			if a.TeamUserID != "" {
				line += ":" + a.TeamUserID
			}
			accessLines = append(accessLines, line)
		}
	}
	if len(accessLines) > 0 {
		_ = writeAtomicFile("access_tokens.txt", []byte(strings.Join(accessLines, "\n")+"\n"))
	}

	// 导出 kami_accounts.txt
	var kamiList []accounts.KamiAccount
	for _, a := range accs {
		if a.Email != "" && a.Password != "" {
			kamiList = append(kamiList, accounts.KamiAccount{
				Email:      a.Email,
				Password:   a.Password,
				TOTPSecret: a.TOTPSecret,
			})
		}
	}
	if len(kamiList) > 0 {
		_ = writeAtomicFile("kami_accounts.txt", []byte(accounts.FormatKamiLines(kamiList)))
	}

	return len(accs), nil
}

func writeAtomicFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		return os.WriteFile(path, data, 0644)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
