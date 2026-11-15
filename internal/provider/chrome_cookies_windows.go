//go:build windows

package provider

import (
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unsafe"

	_ "github.com/glebarez/go-sqlite"
	"golang.org/x/sys/windows"
)

func extractLocalChromeMuseCookies() (string, int, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return "", 0, errors.New("未找到 LOCALAPPDATA 环境变量，无法定位本机 Chrome 数据目录")
	}

	userDataDirs := []string{
		filepath.Join(localAppData, "Google", "Chrome", "User Data"),
		filepath.Join(localAppData, "Microsoft", "Edge", "User Data"),
	}

	var sawV20 bool
	var lastErr error

	for _, userDataDir := range userDataDirs {
		if _, err := os.Stat(userDataDir); err != nil {
			continue
		}

		masterKey, err := readWindowsChromeMasterKey(filepath.Join(userDataDir, "Local State"))
		if err != nil {
			lastErr = err
		}

		profiles := discoverChromeProfiles(userDataDir)
		for _, profileDir := range profiles {
			cookieDB := findProfileCookiesDB(profileDir)
			if cookieDB == "" {
				continue
			}
			cookies, v20Found, err := readMuseCookiesFromSQLite(cookieDB, masterKey)
			if v20Found {
				sawV20 = true
			}
			if err != nil {
				lastErr = err
				continue
			}
			if len(cookies) > 0 {
				return strings.Join(cookies, "; "), len(cookies), nil
			}
		}
	}

	if sawV20 {
		return "", 0, errors.New("检测到新版 Chrome 启用了 App-Bound 加密 (v20)，无法直接解密本地数据库；请点击「启动授权浏览器」完成登录以自动获取完整 Cookie（含 HttpOnly）")
	}
	if lastErr != nil {
		return "", 0, fmt.Errorf("未在本地 Chrome / Edge 中读取到 muse.ai 会话凭据 (%v)", lastErr)
	}
	return "", 0, errors.New("未在本地 Chrome / Edge 中检测到已登录的 muse.ai 会话凭据")
}

func discoverChromeProfiles(userDataDir string) []string {
	var profiles []string
	defaultDir := filepath.Join(userDataDir, "Default")
	if info, err := os.Stat(defaultDir); err == nil && info.IsDir() {
		profiles = append(profiles, defaultDir)
	}
	entries, err := os.ReadDir(userDataDir)
	if err != nil {
		return profiles
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "Profile ") {
			profiles = append(profiles, filepath.Join(userDataDir, name))
		}
	}
	return profiles
}

func findProfileCookiesDB(profileDir string) string {
	candidates := []string{
		filepath.Join(profileDir, "Network", "Cookies"),
		filepath.Join(profileDir, "Cookies"),
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func readWindowsChromeMasterKey(localStatePath string) ([]byte, error) {
	raw, err := os.ReadFile(localStatePath)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if parsed.OSCrypt.EncryptedKey == "" {
		return nil, errors.New("Local State 缺少 os_crypt.encrypted_key")
	}
	decoded, err := base64.StdEncoding.DecodeString(parsed.OSCrypt.EncryptedKey)
	if err != nil {
		return nil, err
	}
	if len(decoded) <= 5 || string(decoded[:5]) != "DPAPI" {
		return nil, errors.New("无效的 DPAPI 密钥头")
	}
	return dpapiUnprotect(decoded[5:])
}

func dpapiUnprotect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty encrypted key")
	}
	var inBlob windows.DataBlob
	inBlob.Size = uint32(len(data))
	inBlob.Data = &data[0]

	var outBlob windows.DataBlob
	if err := windows.CryptUnprotectData(&inBlob, nil, nil, 0, nil, 0, &outBlob); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))

	out := make([]byte, outBlob.Size)
	copy(out, unsafe.Slice(outBlob.Data, outBlob.Size))
	return out, nil
}

// copySharedLockedFile 使用 FILE_SHARE_READ|WRITE|DELETE 复制可能被 Chrome 占用的 Cookies 数据库。
func copySharedLockedFile(src string) (string, error) {
	srcPtr, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		srcPtr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)

	tmpFile, err := os.CreateTemp("", "muse-chrome-cookies-*.db")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	defer tmpFile.Close()

	srcFile := os.NewFile(uintptr(handle), src)
	if _, err := io.Copy(tmpFile, srcFile); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

func readMuseCookiesFromSQLite(cookieDBPath string, masterKey []byte) ([]string, bool, error) {
	tmpPath, err := copySharedLockedFile(cookieDBPath)
	if err != nil {
		return nil, false, err
	}
	defer os.Remove(tmpPath)

	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		return nil, false, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT name, value, encrypted_value FROM cookies WHERE host_key LIKE '%muse.ai%'`)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	seen := make(map[string]string)
	var order []string
	var sawV20 bool

	for rows.Next() {
		var name, plainVal string
		var encVal []byte
		if err := rows.Scan(&name, &plainVal, &encVal); err != nil {
			continue
		}
		val := strings.TrimSpace(plainVal)
		if val == "" && len(encVal) > 0 {
			if len(encVal) >= 3 && string(encVal[:3]) == "v20" {
				sawV20 = true
				continue
			}
			if len(masterKey) > 0 && len(encVal) >= 3+12+16 && (string(encVal[:3]) == "v10" || string(encVal[:3]) == "v11") {
				decrypted, decErr := decryptChromeAESGCM(masterKey, encVal)
				if decErr == nil {
					val = cleanDecryptedCookieValue(decrypted)
				}
			}
		}
		if name == "" || val == "" {
			continue
		}
		if _, exists := seen[name]; !exists {
			order = append(order, name)
		}
		seen[name] = val
	}

	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, fmt.Sprintf("%s=%s", name, seen[name]))
	}
	return parts, sawV20, nil
}

func decryptChromeAESGCM(key, enc []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := enc[3:15]
	ciphertext := enc[15:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func cleanDecryptedCookieValue(raw []byte) string {
	if len(raw) > 32 {
		hasBinaryPrefix := false
		for _, b := range raw[:32] {
			if b < 0x20 || b >= 0x7f {
				hasBinaryPrefix = true
				break
			}
		}
		if hasBinaryPrefix {
			raw = raw[32:]
		}
	}
	s := string(raw)
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	})
}

// startVisibleChromeProcess 在 Windows 上显式使用 SW_SHOWNORMAL 启动 Chrome，
// 避免父进程（如 IDE 终端或后台任务）带有 SW_HIDE 导致子窗口被隐藏。
func startVisibleChromeProcess(chromePath string, args []string) (func(), error) {
	var quoted []string
	quoted = append(quoted, windows.EscapeArg(chromePath))
	for _, a := range args {
		quoted = append(quoted, windows.EscapeArg(a))
	}
	cmdLine, err := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		return nil, err
	}

	desktop, _ := windows.UTF16PtrFromString(`winsta0\default`)
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Desktop = desktop
	si.Flags = windows.STARTF_USESHOWWINDOW
	si.ShowWindow = windows.SW_SHOWNORMAL

	var pi windows.ProcessInformation
	if err := windows.CreateProcess(nil, cmdLine, nil, nil, false, 0, nil, nil, &si, &pi); err != nil {
		return nil, err
	}
	_ = windows.CloseHandle(pi.Thread)

	cleanup := func() {
		_ = windows.TerminateProcess(pi.Process, 0)
		_ = windows.CloseHandle(pi.Process)
	}
	return cleanup, nil
}
