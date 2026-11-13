package accounts

import (
	"testing"
	"time"
)

func TestGenerateTOTP_RFC6238Vector(t *testing.T) {
	// RFC 6238 Appendix B SHA1 test vector: secret = "12345678901234567890" -> Base32 "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, rem, err := GenerateTOTP(secret, time.Unix(59, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "287082" {
		t.Fatalf("expected 287082 at t=59, got %s", code)
	}
	if rem != 1 {
		t.Fatalf("expected remaining 1s at t=59, got %d", rem)
	}
}

func TestGenerateTOTP_UnpaddedLowercaseWithSpaces(t *testing.T) {
	code, rem, err := GenerateTOTP("  opwo lpw6 kqmm 6k4v 674h aie6 ys2l 56nr ", time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("expected 6-digit code, got %q", code)
	}
	if rem <= 0 || rem > 30 {
		t.Fatalf("unexpected remaining seconds: %d", rem)
	}
}
