//go:build !windows

package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

func startVisibleChromeProcess(chromePath string, args []string) (func(), error) {
	cmd := exec.Command(chromePath, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	cleanup := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_, _ = cmd.Process.Wait()
	}
	return cleanup, nil
}

// extractLocalChromeMuseCookies 探测并从本机 Chrome / Chromium (Linux / macOS) 读取解密后的 muse.ai 会话 Cookie。
func extractLocalChromeMuseCookies() (string, int, error) {
	pyScript := `
import os, sys, sqlite3, shutil, json, tempfile, glob, subprocess, hashlib

def find_cookie_files():
    bases = [
        os.path.expanduser('~/.config/google-chrome'),
        os.path.expanduser('~/.config/chromium'),
        os.path.expanduser('~/Library/Application Support/Google/Chrome'),
        os.path.expanduser('~/Library/Application Support/Microsoft Edge'),
    ]
    found = []
    for base in bases:
        if not os.path.isdir(base):
            continue
        profiles = [os.path.join(base, 'Default')] + sorted(glob.glob(os.path.join(base, 'Profile *')))
        for prof in profiles:
            for rel in ('Network/Cookies', 'Cookies'):
                p = os.path.join(prof, rel)
                if os.path.isfile(p):
                    found.append((base, p))
    return found

def get_key():
    if sys.platform == 'darwin':
        for service in ('Chrome Safe Storage', 'Microsoft Edge Safe Storage'):
            try:
                pw = subprocess.check_output(['security', 'find-generic-password', '-w', '-s', service], stderr=subprocess.DEVNULL).strip()
                if pw:
                    return hashlib.pbkdf2_hmac('sha1', pw, b'saltysalt', 1003, 16)
            except Exception:
                pass
        return None
    chrome_secret = None
    try:
        import secretstorage
        conn = secretstorage.dbus_init()
        collection = secretstorage.get_default_collection(conn)
        items = collection.search_items({'application': 'chrome', 'xdg:schema': 'chrome_libsecret_os_crypt_password_v2'})
        for item in items:
            chrome_secret = item.get_secret()
            break
    except Exception:
        pass
    if not chrome_secret:
        chrome_secret = b'peanuts'
    return hashlib.pbkdf2_hmac('sha1', chrome_secret, b'saltysalt', 1, 16)

def get_muse_cookie():
    cookie_files = find_cookie_files()
    if not cookie_files:
        return None
    try:
        from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
    except Exception:
        return None
    key = get_key()
    if not key:
        return None

    iv = b' ' * 16
    seen = {}
    order = []
    for _, cookie_file in cookie_files:
        tmp_db = os.path.join(tempfile.gettempdir(), f'_chrome_cookies_sync_{os.getpid()}.db')
        try:
            shutil.copyfile(cookie_file, tmp_db)
            db_conn = sqlite3.connect(tmp_db)
            c = db_conn.cursor()
            c.execute('SELECT name, value, encrypted_value FROM cookies WHERE host_key LIKE "%muse.ai%"')
            rows = c.fetchall()
            db_conn.close()
        except Exception:
            rows = []
        finally:
            try:
                os.remove(tmp_db)
            except Exception:
                pass

        for name, plain, enc in rows:
            val = (plain or '').strip()
            if not val and enc and enc[:3] in (b'v10', b'v11'):
                try:
                    cipher = Cipher(algorithms.AES(key), modes.CBC(iv))
                    decryptor = cipher.decryptor()
                    dec = decryptor.update(enc[3:]) + decryptor.finalize()
                    pad = dec[-1]
                    raw = dec[:-pad] if 1 <= pad <= 16 else dec
                    if len(raw) > 32 and any(b < 0x20 or b >= 0x7f for b in raw[:32]):
                        raw = raw[32:]
                    val = raw.decode('utf-8', errors='ignore').strip()
                except Exception:
                    val = ''
            if name and val:
                if name not in seen:
                    order.append(name)
                seen[name] = val

    if order:
        return '; '.join(f'{k}={seen[k]}' for k in order)
    return None

c = get_muse_cookie()
if c:
    count = len(c.split('; '))
    print(json.dumps({'ok': True, 'cookie': c, 'count': count}))
else:
    print(json.dumps({'ok': False, 'error': 'not_found'}))
`
	pyBin := "python3"
	if _, err := exec.LookPath(pyBin); err != nil {
		if _, err2 := exec.LookPath("python"); err2 == nil {
			pyBin = "python"
		}
	}
	cmd := exec.Command(pyBin, "-c", pyScript)
	out, err := cmd.Output()
	if err != nil {
		return "", 0, fmt.Errorf("执行本地 Chrome 凭据读取失败: %w", err)
	}

	var res struct {
		OK     bool   `json:"ok"`
		Cookie string `json:"cookie"`
		Count  int    `json:"count"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return "", 0, fmt.Errorf("解析本地 Chrome 凭据输出失败: %w", err)
	}
	if !res.OK || res.Cookie == "" {
		return "", 0, errors.New("未在本地 Chrome 中检测到已登录的 muse.ai 会话凭据")
	}
	return res.Cookie, res.Count, nil
}
