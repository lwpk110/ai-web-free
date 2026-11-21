package db

import (
	"strings"
	"testing"
)

func TestSystemSettingsCRUDAndEncryption(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	const secret = "unit-test-admin-secret"

	// 1. 写入 global 配置（含明文项与敏感加密项）
	err = UpsertSystemSettings(database, "global", []SystemSettingUpsertItem{
		{Key: "STREAM_MODE", Category: "gateway", Value: "false", IsSecret: false},
		{Key: "SILICONFLOW_API_KEY", Category: "multimodal", Value: "sk-silicon-1234567890abcdef", IsSecret: true},
	}, secret, "admin")
	if err != nil {
		t.Fatalf("UpsertSystemSettings global failed: %v", err)
	}

	globalMap, err := LoadSystemSettingsByScope(database, "global")
	if err != nil {
		t.Fatalf("LoadSystemSettingsByScope failed: %v", err)
	}
	if globalMap["STREAM_MODE"].Value != "false" {
		t.Fatalf("expected STREAM_MODE=false, got %q", globalMap["STREAM_MODE"].Value)
	}
	secRec := globalMap["SILICONFLOW_API_KEY"]
	if secRec.Value != "" || !strings.HasPrefix(secRec.ValueCipher, settingCipherPrefix) {
		t.Fatalf("expected secret stored in ValueCipher with prefix, got value=%q cipher=%q", secRec.Value, secRec.ValueCipher)
	}

	// 2. 写入 vps Profile 覆盖项，验证与 global 隔离且 Reveal 正常回退与覆盖
	err = UpsertSystemSettings(database, "vps", []SystemSettingUpsertItem{
		{Key: "CLASH_SUB_URL", Category: "proxy", Value: "https://airport.example.com/sub?token=secret999", IsSecret: true},
	}, secret, "admin")
	if err != nil {
		t.Fatalf("UpsertSystemSettings vps failed: %v", err)
	}

	// vps 下查询全局 Key 应自动回退解密到 global
	plainSF, resolvedScope, found, err := RevealSystemSetting(database, "vps", "SILICONFLOW_API_KEY", secret)
	if err != nil || !found || resolvedScope != "global" || plainSF != "sk-silicon-1234567890abcdef" {
		t.Fatalf("Reveal fallback failed: found=%v scope=%q val=%q err=%v", found, resolvedScope, plainSF, err)
	}

	plainSub, resolvedScope2, found2, err := RevealSystemSetting(database, "vps", "CLASH_SUB_URL", secret)
	if err != nil || !found2 || resolvedScope2 != "vps" || plainSub != "https://airport.example.com/sub?token=secret999" {
		t.Fatalf("Reveal vps secret failed: found=%v scope=%q val=%q err=%v", found2, resolvedScope2, plainSub, err)
	}

	// 3. 删除覆盖项验证回退
	if err := DeleteSystemSetting(database, "vps", "CLASH_SUB_URL"); err != nil {
		t.Fatalf("DeleteSystemSetting failed: %v", err)
	}
	_, _, foundAfterDel, _ := RevealSystemSetting(database, "vps", "CLASH_SUB_URL", secret)
	if foundAfterDel {
		t.Fatalf("expected CLASH_SUB_URL deleted in vps scope")
	}

	// 4. 掩码函数测试
	masked := MaskSecretValue("sk-silicon-1234567890abcdef")
	if !strings.HasPrefix(masked, "sk-sil") || !strings.HasSuffix(masked, "cdef") || strings.Contains(masked, "1234567890") {
		t.Fatalf("unexpected masked output: %q", masked)
	}
}
