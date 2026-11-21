package db

import (
	"testing"
)

func TestProxiesAndRestrictionsCRUD(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// 1. 代理节点 CRUD 测试
	urls := []string{
		"http://127.0.0.1:7890",
		"socks5://10.0.0.1:1080",
	}
	if err := SaveProxiesToDB(database, urls); err != nil {
		t.Fatalf("SaveProxiesToDB failed: %v", err)
	}
	loaded, err := LoadProxiesFromDB(database)
	if err != nil {
		t.Fatalf("LoadProxiesFromDB failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(loaded))
	}
	if err := DeleteProxyFromDB(database, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("DeleteProxyFromDB failed: %v", err)
	}
	loadedAfterDel, _ := LoadProxiesFromDB(database)
	if len(loadedAfterDel) != 1 || loadedAfterDel[0] != "socks5://10.0.0.1:1080" {
		t.Fatalf("expected 1 proxy remaining, got %v", loadedAfterDel)
	}

	// 2. 账号限制名单（黑名单/禁用）CRUD 测试
	if err := SaveRestrictionToDB(database, "banned@test.com", "banned", "风控停用"); err != nil {
		t.Fatalf("SaveRestrictionToDB banned failed: %v", err)
	}
	if err := SaveRestrictionToDB(database, "disabled@test.com", "disabled", "手工禁用"); err != nil {
		t.Fatalf("SaveRestrictionToDB disabled failed: %v", err)
	}

	bannedMap := LoadRestrictionsFromDB(database, "banned")
	if bannedMap["banned@test.com"] != "风控停用" {
		t.Errorf("expected banned reason, got %v", bannedMap)
	}
	disabledMap := LoadRestrictionsFromDB(database, "disabled")
	if disabledMap["disabled@test.com"] != "手工禁用" {
		t.Errorf("expected disabled reason, got %v", disabledMap)
	}

	if err := DeleteRestrictionFromDB(database, "banned@test.com", "banned"); err != nil {
		t.Fatalf("DeleteRestrictionFromDB failed: %v", err)
	}
	bannedAfterDel := LoadRestrictionsFromDB(database, "banned")
	if _, ok := bannedAfterDel["banned@test.com"]; ok {
		t.Errorf("expected banned to be deleted")
	}

	// 3. 多 Profile (home / office / vps) 代理节点隔离与专属粘性映射 CRUD 测试
	if err := ReplaceProxiesByProfile(database, "home", []string{"http://127.0.0.1:10809"}); err != nil {
		t.Fatalf("ReplaceProxiesByProfile home failed: %v", err)
	}
	if err := ReplaceProxiesByProfile(database, "office", []string{"http://192.168.2.12:10809"}); err != nil {
		t.Fatalf("ReplaceProxiesByProfile office failed: %v", err)
	}
	if err := ReplaceClashProxiesByProfile(database, "vps", []ProxyNodeRecord{
		{URL: "http://127.0.0.1:20001", NodeName: "🇺🇸 美国 01", Region: "US", Port: 20001},
		{URL: "http://127.0.0.1:20002", NodeName: "🇯🇵 日本 01", Region: "JP", Port: 20002},
	}); err != nil {
		t.Fatalf("ReplaceClashProxiesByProfile vps failed: %v", err)
	}

	homeProxies, err := LoadProxiesByProfile(database, "home")
	if err != nil || len(homeProxies) != 1 || homeProxies[0] != "http://127.0.0.1:10809" {
		t.Fatalf("unexpected home proxies: %v (err=%v)", homeProxies, err)
	}
	officeProxies, err := LoadProxiesByProfile(database, "office")
	if err != nil || len(officeProxies) != 1 || officeProxies[0] != "http://192.168.2.12:10809" {
		t.Fatalf("unexpected office proxies: %v (err=%v)", officeProxies, err)
	}
	vpsRecords, err := LoadProxyRecordsByProfile(database, "vps")
	if err != nil || len(vpsRecords) != 2 || vpsRecords[0].NodeName != "🇺🇸 美国 01" || vpsRecords[0].Port != 20001 {
		t.Fatalf("unexpected vps proxy records: %+v (err=%v)", vpsRecords, err)
	}

	// 验证同一账号在不同 Profile 下拥有独立的粘性代理绑定且互不覆盖
	if err := SaveAccountProfileProxy(database, "home", "user1@example.com", "http://127.0.0.1:10809"); err != nil {
		t.Fatalf("SaveAccountProfileProxy home failed: %v", err)
	}
	if err := SaveAccountProfileProxy(database, "office", "user1@example.com", "http://192.168.2.12:10809"); err != nil {
		t.Fatalf("SaveAccountProfileProxy office failed: %v", err)
	}
	if err := SaveAccountProfileProxy(database, "vps", "user1@example.com", "http://127.0.0.1:20002"); err != nil {
		t.Fatalf("SaveAccountProfileProxy vps failed: %v", err)
	}

	if got := LoadAccountProfileProxies(database, "home")["user1@example.com"]; got != "http://127.0.0.1:10809" {
		t.Fatalf("expected home sticky proxy http://127.0.0.1:10809, got %q", got)
	}
	if got := LoadAccountProfileProxies(database, "office")["user1@example.com"]; got != "http://192.168.2.12:10809" {
		t.Fatalf("expected office sticky proxy http://192.168.2.12:10809, got %q", got)
	}
	if got := LoadAccountProfileProxies(database, "vps")["user1@example.com"]; got != "http://127.0.0.1:20002" {
		t.Fatalf("expected vps sticky proxy http://127.0.0.1:20002, got %q", got)
	}
}
