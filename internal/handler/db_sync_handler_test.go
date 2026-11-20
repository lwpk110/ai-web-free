package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/db"
	"ai-web-free/internal/health"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openHandlerTestSQLite(t *testing.T, name string) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), name)
	gdb, err := gorm.Open(sqlite.Open(dbPath+"?_pragma=busy_timeout(5000)"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		t.Fatalf("open sqlite %s: %v", name, err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() {
			_ = sqlDB.Close()
		})
	}
	models := append(db.CoreModels(), &db.SyncTombstoneRecord{}, &db.SyncJournalRecord{})
	if err := gdb.AutoMigrate(models...); err != nil {
		t.Fatalf("automigrate %s: %v", name, err)
	}
	return gdb
}

func TestAdminDBSyncEndpointsAndSystemStatus(t *testing.T) {
	pool := accounts.NewPool(nil)
	proxyPool := proxy.NewPool(nil)
	reg := provider.NewRegistry()
	cfgDisabled := &config.Config{
		AdminToken:  "adm-token",
		DatabaseURL: "data/ai-web-free.db",
	}

	// 1. 未开启物化同步时：GET status 返回 disabled，POST trigger 返回 400
	routerDisabled := RegisterRouterWithDeps(RouterDeps{
		AccountPool: pool,
		ProxyPool:   proxyPool,
		Registry:    reg,
		Config:      cfgDisabled,
	})

	wUnauth := httptest.NewRecorder()
	routerDisabled.ServeHTTP(wUnauth, httptest.NewRequest(http.MethodGet, "/admin/api/v1/db/sync/status", nil))
	if wUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without admin token, got %d", wUnauth.Code)
	}

	reqSt := httptest.NewRequest(http.MethodGet, "/admin/api/v1/db/sync/status", nil)
	reqSt.Header.Set("Authorization", "Bearer adm-token")
	wSt := httptest.NewRecorder()
	routerDisabled.ServeHTTP(wSt, reqSt)
	if wSt.Code != http.StatusOK {
		t.Fatalf("expected 200 for disabled status, got %d: %s", wSt.Code, wSt.Body.String())
	}
	var stDisabled db.SyncStatus
	if err := json.Unmarshal(wSt.Body.Bytes(), &stDisabled); err != nil {
		t.Fatalf("unmarshal disabled status: %v", err)
	}
	if stDisabled.Enabled || stDisabled.Mode != "local_only" || stDisabled.State != "disabled" {
		t.Fatalf("unexpected disabled status: %+v", stDisabled)
	}

	reqTrigDisabled := httptest.NewRequest(http.MethodPost, "/admin/api/v1/db/sync/trigger", bytes.NewBufferString(`{"direction":"both"}`))
	reqTrigDisabled.Header.Set("Authorization", "Bearer adm-token")
	reqTrigDisabled.Header.Set("Content-Type", "application/json")
	wTrigDisabled := httptest.NewRecorder()
	routerDisabled.ServeHTTP(wTrigDisabled, reqTrigDisabled)
	if wTrigDisabled.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when materialize disabled, got %d", wTrigDisabled.Code)
	}

	// 2. 开启物化同步后：验证 status 脱敏、trigger 执行推送、system/status 包含 database meta
	localDB := openHandlerTestSQLite(t, "handler_local.db")
	cloudDB := openHandlerTestSQLite(t, "handler_cloud.db")

	cfgEnabled := &config.Config{
		AdminToken:           "adm-token",
		DatabaseURL:          "postgres://user:top-secret-pw@ep-demo-pooler.us-east-2.aws.neon.tech:5432/neondb?sslmode=require",
		DBMaterializeEnabled: true,
		DBLocalPath:          "handler_local.db",
		DBSyncPushInterval:   5 * time.Second,
		DBSyncTimeout:        10 * time.Second,
	}

	eng := db.NewSyncEngine(localDB, cfgEnabled.DatabaseURL, db.SyncConfig{
		Enabled:      true,
		LocalPath:    cfgEnabled.DBLocalPath,
		PushInterval: cfgEnabled.DBSyncPushInterval,
		BatchSize:    50,
		Timeout:      cfgEnabled.DBSyncTimeout,
	})
	eng.RegisterTables(db.DefaultSyncTables()...)
	eng.SetCloudOpener(func(ctx context.Context, dsn string, extraModels ...any) (*gorm.DB, error) {
		return cloudDB, nil
	})
	if err := eng.PrepareLocal(); err != nil {
		t.Fatalf("PrepareLocal: %v", err)
	}

	// 在本地写入一条记录，应被触发器捕获到 Outbox
	now := time.Now().UTC()
	if err := localDB.Create(&db.PromptFavorite{PromptID: "p-sync-1", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create PromptFavorite: %v", err)
	}

	routerEnabled := RegisterRouterWithDeps(RouterDeps{
		AccountPool:   pool,
		ProxyPool:     proxyPool,
		Registry:      reg,
		Config:        cfgEnabled,
		DB:            localDB,
		SyncEngine:    eng,
		HealthTracker: health.NewTracker(),
	})

	// 非法方向校验
	reqBadDir := httptest.NewRequest(http.MethodPost, "/admin/api/v1/db/sync/trigger", bytes.NewBufferString(`{"direction":"sideways"}`))
	reqBadDir.Header.Set("Authorization", "Bearer adm-token")
	reqBadDir.Header.Set("Content-Type", "application/json")
	wBadDir := httptest.NewRecorder()
	routerEnabled.ServeHTTP(wBadDir, reqBadDir)
	if wBadDir.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid direction, got %d", wBadDir.Code)
	}

	// 手动触发双向同步
	reqTrig := httptest.NewRequest(http.MethodPost, "/admin/api/v1/db/sync/trigger", bytes.NewBufferString(`{"direction":"both"}`))
	reqTrig.Header.Set("Authorization", "Bearer adm-token")
	reqTrig.Header.Set("Content-Type", "application/json")
	wTrig := httptest.NewRecorder()
	routerEnabled.ServeHTTP(wTrig, reqTrig)
	if wTrig.Code != http.StatusOK {
		t.Fatalf("expected 200 on trigger, got %d: %s", wTrig.Code, wTrig.Body.String())
	}

	var trigResp struct {
		OK        bool          `json:"ok"`
		Direction string        `json:"direction"`
		Error     string        `json:"error"`
		Status    db.SyncStatus `json:"status"`
	}
	if err := json.Unmarshal(wTrig.Body.Bytes(), &trigResp); err != nil {
		t.Fatalf("unmarshal trigger resp: %v", err)
	}
	if !trigResp.OK || !trigResp.Status.Enabled || trigResp.Status.Mode != "materialized" || trigResp.Status.OutboxPending != 0 {
		t.Fatalf("unexpected trigger response: %+v", trigResp)
	}
	if strings.Contains(trigResp.Status.CloudTarget, "top-secret-pw") || strings.Contains(trigResp.Status.CloudTarget, "sslmode") {
		t.Fatalf("cloud_target leaked credentials: %q", trigResp.Status.CloudTarget)
	}

	var cloudFavCnt int64
	cloudDB.Model(&db.PromptFavorite{}).Where("prompt_id = ?", "p-sync-1").Count(&cloudFavCnt)
	if cloudFavCnt != 1 {
		t.Fatalf("expected p-sync-1 synced to cloud DB, got count=%d", cloudFavCnt)
	}

	// 校验 /admin/api/v1/system/status 中的 database 子系统包含物化模式 meta
	reqSys := httptest.NewRequest(http.MethodGet, "/admin/api/v1/system/status", nil)
	reqSys.Header.Set("Authorization", "Bearer adm-token")
	wSys := httptest.NewRecorder()
	routerEnabled.ServeHTTP(wSys, reqSys)
	if wSys.Code != http.StatusOK {
		t.Fatalf("expected 200 on system status, got %d", wSys.Code)
	}
	var sysReport health.ServerStatusReport
	if err := json.Unmarshal(wSys.Body.Bytes(), &sysReport); err != nil {
		t.Fatalf("unmarshal system status: %v", err)
	}
	foundDB := false
	for _, sub := range sysReport.Subsystems {
		if sub.Name == "database" {
			foundDB = true
			if sub.Status != "healthy" || sub.Meta["mode"] != "materialized" {
				t.Fatalf("unexpected database subsystem: %+v", sub)
			}
		}
	}
	if !foundDB {
		t.Fatal("missing database subsystem in system status")
	}
}
