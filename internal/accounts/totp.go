package accounts

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// GenerateTOTP 使用原生标准库计算 30 秒周期的 6 位 TOTP 动态码及剩余有效秒数。
func GenerateTOTP(secret string, now time.Time) (string, int, error) {
	cleaned := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	if cleaned == "" {
		return "", 0, fmt.Errorf("empty totp secret")
	}
	if rem := len(cleaned) % 8; rem != 0 {
		cleaned += strings.Repeat("=", 8-rem)
	}

	key, err := base32.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return "", 0, fmt.Errorf("invalid base32 totp secret: %w", err)
	}

	unixSec := now.Unix()
	counter := uint64(unixSec / 30)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)

	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	code := truncated % 1000000
	remaining := 30 - int(unixSec%30)
	return fmt.Sprintf("%06d", code), remaining, nil
}
