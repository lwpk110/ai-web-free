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
}
