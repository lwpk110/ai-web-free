package db

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type testGatewayGroup struct {
	ID             string            `gorm:"primaryKey;size:64"`
	Name           string            `gorm:"size:128;uniqueIndex"`
	ModelAllowlist []string          `gorm:"serializer:json;type:text"`
	ModelAliases   map[string]string `gorm:"serializer:json;type:text"`
	Enabled        bool              `gorm:"default:true"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (testGatewayGroup) TableName() string {
	return "gateway_groups"
}

func openTestSQLite(t *testing.T, name string) *gorm.DB {
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
	return gdb
}

func migrateAllTestModels(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	models := append(CoreModels(), &testGatewayGroup{}, &SyncTombstoneRecord{}, &SyncJournalRecord{})
	if err := gdb.AutoMigrate(models...); err != nil {
		t.Fatalf("automigrate test models: %v", err)
	}
}

func newTestSyncEngine(t *testing.T, localDB, cloudDB *gorm.DB, cfg SyncConfig) *SyncEngine {
	t.Helper()
	migrateAllTestModels(t, localDB)
	if cloudDB != nil {
		migrateAllTestModels(t, cloudDB)
	}
	tables := append(DefaultSyncTables(), NewTypedSyncTable[testGatewayGroup]())
	eng := NewSyncEngine(localDB, "postgres://user:secret-pass@db.example.com:5432/neondb?sslmode=require", cfg)
	eng.RegisterTables(tables...)
	if cloudDB != nil {
		eng.SetCloudOpener(func(ctx context.Context, dsn string, extraModels ...any) (*gorm.DB, error) {
			return cloudDB, nil
		})
	}
	if err := eng.PrepareLocal(); err != nil {
		t.Fatalf("PrepareLocal: %v", err)
	}
	return eng
}

func TestMaskDSNAndIsRemoteDSN(t *testing.T) {
	cases := []struct {
		dsn      string
		isRemote bool
		masked   string
		forbid   []string
	}{
		{
			dsn:      "postgres://alice:super-secret-pw@ep-cool-pooler.us-east-2.aws.neon.tech:5432/neondb?sslmode=require&password=leak",
			isRemote: true,
			masked:   "postgres://ep-cool-pooler.us-east-2.aws.neon.tech:5432/neondb",
			forbid:   []string{"alice", "super-secret-pw", "sslmode", "leak"},
		},
		{
			dsn:      "postgresql://bob:pw123@pg.internal/app",
			isRemote: true,
			masked:   "postgresql://pg.internal/app",
			forbid:   []string{"bob", "pw123"},
		},
		{
			dsn:      "mysql://root:my-pass@tcp(10.0.0.5:3306)/aiwebfree?parseTime=true",
			isRemote: true,
			forbid:   []string{"root", "my-pass", "parseTime"},
		},
		{
			dsn:      "admin:rawpass@tcp(10.0.0.5:3306)/aiwebfree?charset=utf8mb4",
			isRemote: true,
			masked:   "mysql://tcp(10.0.0.5:3306)/aiwebfree",
			forbid:   []string{"admin", "rawpass", "charset"},
		},
		{
			dsn:      "data/ai-web-free.db",
			isRemote: false,
			masked:   "data/ai-web-free.db",
		},
	}

	for _, tc := range cases {
		if got := IsRemoteDSN(tc.dsn); got != tc.isRemote {
			t.Fatalf("IsRemoteDSN(%q) = %v, want %v", tc.dsn, got, tc.isRemote)
		}
		masked := MaskDSN(tc.dsn)
		if tc.masked != "" && masked != tc.masked {
			t.Fatalf("MaskDSN(%q) = %q, want %q", tc.dsn, masked, tc.masked)
		}
		for _, bad := range tc.forbid {
			if strings.Contains(masked, bad) {
				t.Fatalf("MaskDSN(%q) leaked %q in %q", tc.dsn, bad, masked)
			}
		}
	}
}

func TestSQLiteTriggersCaptureAndSuppressLoop(t *testing.T) {
	localDB := openTestSQLite(t, "local_triggers.db")
	eng := newTestSyncEngine(t, localDB, nil, SyncConfig{LocalPath: "local_triggers.db"})

	now := time.Now().UTC().Truncate(time.Second)
	work := GalleryWork{
		ID:        "img-1",
		Prompt:    "sunrise",
		URL:       "/storage/images/img-1.png",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := localDB.Create(&work).Error; err != nil {
		t.Fatalf("insert GalleryWork: %v", err)
	}
	if err := localDB.Model(&GalleryWork{}).Where("id = ?", "img-1").Update("is_favorite", true).Error; err != nil {
		t.Fatalf("update GalleryWork: %v", err)
	}
	if err := localDB.Delete(&GalleryWork{}, "id = ?", "img-1").Error; err != nil {
		t.Fatalf("delete GalleryWork: %v", err)
	}

	// 验证批量清表（AllowGlobalUpdate）也会触发 AFTER DELETE 捕获
	if err := localDB.Create(&[]ProxyNodeRecord{
		{ID: "p1", URL: "http://127.0.0.1:8001", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: "p2", URL: "http://127.0.0.1:8002", Status: "active", CreatedAt: now, UpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("insert proxies: %v", err)
	}
	if err := localDB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&ProxyNodeRecord{}).Error; err != nil {
		t.Fatalf("clear proxies: %v", err)
	}

	var outbox []outboxItem
	if err := localDB.Raw(`SELECT seq, table_name, pk, op, changed_at FROM _sync_outbox ORDER BY seq ASC`).Scan(&outbox).Error; err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if len(outbox) != 7 {
		t.Fatalf("expected 7 outbox entries (3 gallery + 2 proxy insert + 2 proxy delete), got %d: %+v", len(outbox), outbox)
	}
	if outbox[0].Op != "upsert" || outbox[1].Op != "upsert" || outbox[2].Op != "delete" {
		t.Fatalf("unexpected gallery ops: %+v", outbox[:3])
	}
	if outbox[5].Op != "delete" || outbox[6].Op != "delete" {
		t.Fatalf("expected bulk delete triggers, got %+v", outbox[5:])
	}

	// 清空 outbox 后验证 withSuppressedTriggers 不会写入 _sync_outbox
	if err := localDB.Exec(`DELETE FROM _sync_outbox`).Error; err != nil {
		t.Fatalf("clear outbox: %v", err)
	}
	if err := withSuppressedTriggers(localDB, func(tx *gorm.DB) error {
		return tx.Create(&GalleryWork{
			ID:        "img-suppressed",
			Prompt:    "cloud pull",
			URL:       "/storage/images/img-suppressed.png",
			CreatedAt: now,
			UpdatedAt: now,
		}).Error
	}); err != nil {
		t.Fatalf("withSuppressedTriggers: %v", err)
	}
	if pending := eng.countOutboxPending(); pending != 0 {
		t.Fatalf("expected 0 outbox entries during suppressed write, got %d", pending)
	}
}

func TestSyncEnginePushCoalescingAndTypes(t *testing.T) {
	localDB := openTestSQLite(t, "local_push.db")
	cloudDB := openTestSQLite(t, "cloud_push.db")
	eng := newTestSyncEngine(t, localDB, cloudDB, SyncConfig{
		LocalPath: "local_push.db",
		BatchSize: 50,
	})

	// 先标记完成初次水合，聚焦测试增量写入合并推送
	ctx := context.Background()
	if err := eng.SyncOnce(ctx, SyncDirectionBoth); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	// 1. 同一主键 Insert -> Update -> Delete 应合并为云端 Delete + 写入墓碑
	if err := localDB.Create(&ChatPreset{
		ID: "pre-1", Name: "v1", SystemPrompt: "p", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := localDB.Model(&ChatPreset{}).Where("id = ?", "pre-1").Update("name", "v2").Error; err != nil {
		t.Fatal(err)
	}
	if err := localDB.Delete(&ChatPreset{}, "id = ?", "pre-1").Error; err != nil {
		t.Fatal(err)
	}

	// 2. 验证 BlobData 二进制、Tags JSON 切片、bool false 覆盖 default:true 均无损同步
	blob := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x01, 0x02}
	gw := GalleryWork{
		ID:        "img-blob",
		Prompt:    "cyber city",
		Model:     "gpt-image-1",
		Tags:      []string{"neon", "city"},
		BlobData:  blob,
		URL:       "/storage/images/img-blob.png",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := localDB.Create(&gw).Error; err != nil {
		t.Fatal(err)
	}

	// 带有 gorm:"default:true" 的布尔 false 字段必须原样保留为 false（禁止被 GORM 覆写为 true）
	pat := CredentialFormatPatternRecord{
		ID:             "pat-disabled",
		Provider:       "grok",
		CredentialType: "sso",
		InputTemplate:  "email|pass",
		OutputTemplate: "{email}----{pass}",
		Enabled:        false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := localDB.Create(&pat).Error; err != nil {
		t.Fatal(err)
	}
	if err := localDB.Model(&CredentialFormatPatternRecord{}).Where("id = ?", "pat-disabled").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	grp := testGatewayGroup{
		ID:             "grp-1",
		Name:           "free-pool",
		ModelAllowlist: []string{"gpt-4o", "gpt-5-mini"},
		ModelAliases:   map[string]string{"alias-4o": "gpt-4o"},
		Enabled:        false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := localDB.Create(&grp).Error; err != nil {
		t.Fatal(err)
	}
	if err := localDB.Model(&testGatewayGroup{}).Where("id = ?", "grp-1").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	// VideoTaskRecord 含新加的 UpdatedAt 字段
	vt := VideoTaskRecord{
		ID:        "vid-1",
		TaskID:    "t-1",
		Provider:  "siliconflow",
		Model:     "cogvideox",
		Prompt:    "ocean wave",
		Status:    "SUCCEEDED",
		CreatedAt: now,
		UpdatedAt: now.Add(5 * time.Second),
	}
	if err := localDB.Create(&vt).Error; err != nil {
		t.Fatal(err)
	}

	if err := eng.SyncOnce(ctx, SyncDirectionPush); err != nil {
		t.Fatalf("push sync: %v", err)
	}
	if pending := eng.countOutboxPending(); pending != 0 {
		t.Fatalf("expected outbox drained after push, got %d", pending)
	}

	// 验证 pre-1 已在云端创建墓碑且主表不存在
	var presetCnt int64
	cloudDB.Model(&ChatPreset{}).Where("id = ?", "pre-1").Count(&presetCnt)
	if presetCnt != 0 {
		t.Fatalf("expected pre-1 absent in cloud, got %d", presetCnt)
	}
	var tomb SyncTombstoneRecord
	if err := cloudDB.First(&tomb, "table_name = ? AND pk = ?", "chat_presets", "pre-1").Error; err != nil {
		t.Fatalf("expected tombstone for pre-1 in cloud: %v", err)
	}

	// 验证 GalleryWork BlobData & Tags
	var cloudGW GalleryWork
	if err := cloudDB.First(&cloudGW, "id = ?", "img-blob").Error; err != nil {
		t.Fatalf("cloud GalleryWork missing: %v", err)
	}
	if !reflect.DeepEqual(cloudGW.BlobData, blob) {
		t.Fatalf("BlobData mismatch: got %v, want %v", cloudGW.BlobData, blob)
	}
	if !reflect.DeepEqual(cloudGW.Tags, []string{"neon", "city"}) {
		t.Fatalf("Tags mismatch: got %v", cloudGW.Tags)
	}

	// 验证 bool false 没有被 default:true 改写
	var cloudPat CredentialFormatPatternRecord
	if err := cloudDB.First(&cloudPat, "id = ?", "pat-disabled").Error; err != nil {
		t.Fatal(err)
	}
	if cloudPat.Enabled {
		t.Fatalf("expected CredentialFormatPatternRecord.Enabled == false in cloud")
	}

	var cloudGrp testGatewayGroup
	if err := cloudDB.First(&cloudGrp, "id = ?", "grp-1").Error; err != nil {
		t.Fatal(err)
	}
	if cloudGrp.Enabled || !reflect.DeepEqual(cloudGrp.ModelAllowlist, []string{"gpt-4o", "gpt-5-mini"}) || cloudGrp.ModelAliases["alias-4o"] != "gpt-4o" {
		t.Fatalf("cloud testGatewayGroup mismatch: %+v", cloudGrp)
	}

	// 验证 Delete -> Re-Insert 归并为 Upsert 且清除墓碑
	if err := localDB.Create(&ChatPreset{
		ID: "pre-1", Name: "reborn", SystemPrompt: "p2", CreatedAt: now, UpdatedAt: now.Add(10 * time.Second),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := eng.SyncOnce(ctx, SyncDirectionPush); err != nil {
		t.Fatalf("push reborn preset: %v", err)
	}
	var tombCnt int64
	cloudDB.Model(&SyncTombstoneRecord{}).Where("table_name = ? AND pk = ?", "chat_presets", "pre-1").Count(&tombCnt)
	if tombCnt != 0 {
		t.Fatalf("expected tombstone cleared after re-insert, got %d", tombCnt)
	}
	var cloudPre ChatPreset
	if err := cloudDB.First(&cloudPre, "id = ?", "pre-1").Error; err != nil || cloudPre.Name != "reborn" {
		t.Fatalf("expected reborn preset in cloud, err=%v got=%+v", err, cloudPre)
	}
}

func TestSyncEngineBidirectionalLWWAndTombstones(t *testing.T) {
	cloudDB := openTestSQLite(t, "shared_cloud.db")
	localA := openTestSQLite(t, "node_a.db")
	localB := openTestSQLite(t, "node_b.db")

	var remoteChangedA map[string]int
	var remoteChangedB map[string]int
	engA := newTestSyncEngine(t, localA, cloudDB, SyncConfig{
		LocalPath: "node_a.db",
		OnRemoteChanged: func(c map[string]int) {
			remoteChangedA = c
		},
	})
	engB := newTestSyncEngine(t, localB, cloudDB, SyncConfig{
		LocalPath: "node_b.db",
		OnRemoteChanged: func(c map[string]int) {
			remoteChangedB = c
		},
	})
	_ = remoteChangedA

	ctx := context.Background()
	t0 := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)

	// 节点 A 创建两条账号记录并推送
	if err := localA.Create(&[]PoolAccountRecord{
		{ID: "acc-1", Email: "a@example.com", Status: "active", CreatedAt: t0, UpdatedAt: t0},
		{ID: "acc-2", Email: "b@example.com", Status: "active", CreatedAt: t0, UpdatedAt: t0},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := engA.SyncOnce(ctx, SyncDirectionBoth); err != nil {
		t.Fatalf("engA sync: %v", err)
	}

	// 节点 B 首次拉取应自动水合并触发 OnRemoteChanged
	if err := engB.SyncOnce(ctx, SyncDirectionBoth); err != nil {
		t.Fatalf("engB initial sync: %v", err)
	}
	if remoteChangedB["pool_account_records"] != 2 {
		t.Fatalf("expected OnRemoteChanged pool_account_records=2 on node B, got %+v", remoteChangedB)
	}

	// LWW 冲突：节点 A 在 t0+2m 更新 acc-1，节点 B 在 t0+5m 更新 acc-1（节点 B 更晚）
	tA := t0.Add(2 * time.Minute)
	tB := t0.Add(5 * time.Minute)
	if err := localA.Model(&PoolAccountRecord{}).Where("id = ?", "acc-1").
		Updates(map[string]any{"status": "expired", "updated_at": tA}).Error; err != nil {
		t.Fatal(err)
	}
	if err := engA.SyncOnce(ctx, SyncDirectionPush); err != nil {
		t.Fatal(err)
	}

	if err := localB.Model(&PoolAccountRecord{}).Where("id = ?", "acc-1").
		Updates(map[string]any{"status": "rate_limited", "updated_at": tB}).Error; err != nil {
		t.Fatal(err)
	}
	// 节点 B 执行双向同步：拉取时云端(tA) 较旧不得覆盖本地(tB)，随后推送本地(tB) 覆盖云端
	if err := engB.SyncOnce(ctx, SyncDirectionBoth); err != nil {
		t.Fatal(err)
	}
	// 节点 A 再拉取：云端(tB) 较新应覆盖节点 A
	if err := engA.SyncOnce(ctx, SyncDirectionPull); err != nil {
		t.Fatal(err)
	}

	var gotA, gotB PoolAccountRecord
	_ = localA.First(&gotA, "id = ?", "acc-1")
	_ = localB.First(&gotB, "id = ?", "acc-1")
	if gotA.Status != "rate_limited" || gotB.Status != "rate_limited" {
		t.Fatalf("LWW convergence failed: A=%s B=%s", gotA.Status, gotB.Status)
	}

	// 墓碑删除传播：节点 A 删除 acc-2 并推送；节点 B 拉取后 acc-2 应被删除
	if err := localA.Delete(&PoolAccountRecord{}, "id = ?", "acc-2").Error; err != nil {
		t.Fatal(err)
	}
	if err := engA.SyncOnce(ctx, SyncDirectionPush); err != nil {
		t.Fatal(err)
	}
	if err := engB.SyncOnce(ctx, SyncDirectionPull); err != nil {
		t.Fatal(err)
	}
	var cntB int64
	localB.Model(&PoolAccountRecord{}).Where("id = ?", "acc-2").Count(&cntB)
	if cntB != 0 {
		t.Fatalf("expected acc-2 deleted on node B via tombstone, still found %d", cntB)
	}

	// 若节点 B 以晚于墓碑的时间戳重新写入 acc-2，则后续 Pull 不得误删
	futureTime := time.Now().UTC().Add(1 * time.Hour)
	if err := localB.Create(&PoolAccountRecord{
		ID: "acc-2", Email: "b@example.com", Status: "active", CreatedAt: futureTime, UpdatedAt: futureTime,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := engB.SyncOnce(ctx, SyncDirectionPull); err != nil {
		t.Fatal(err)
	}
	localB.Model(&PoolAccountRecord{}).Where("id = ?", "acc-2").Count(&cntB)
	if cntB != 1 {
		t.Fatalf("expected newer local acc-2 preserved over older tombstone, got %d", cntB)
	}
}

func TestSyncEngineCloudFailureBackoffAndRecovery(t *testing.T) {
	localDB := openTestSQLite(t, "local_offline.db")
	cloudDB := openTestSQLite(t, "cloud_recovered.db")

	failCloud := true
	eng := newTestSyncEngine(t, localDB, cloudDB, SyncConfig{
		LocalPath:    "local_offline.db",
		PushInterval: 50 * time.Millisecond,
	})
	eng.SetCloudOpener(func(ctx context.Context, dsn string, extraModels ...any) (*gorm.DB, error) {
		if failCloud {
			return nil, errors.New("simulated network timeout")
		}
		return cloudDB, nil
	})

	ctx := context.Background()
	if err := eng.SyncOnce(ctx, SyncDirectionBoth); err == nil {
		t.Fatal("expected initial sync failure while cloud offline")
	}
	st := eng.Status()
	if st.State != "degraded" || st.ConsecutiveFailures != 1 || st.NextRetryAt == "" {
		t.Fatalf("expected degraded status with backoff, got %+v", st)
	}

	// 云端不可用期间，本地 SQLite 读写完全正常
	now := time.Now().UTC()
	if err := localDB.Create(&FreebieDeal{
		ID: "deal-1", Title: "Free Credits", URL: "https://example.com/deal-1", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("local write during cloud outage failed: %v", err)
	}
	if pending := eng.countOutboxPending(); pending != 1 {
		t.Fatalf("expected 1 outbox item queued during outage, got %d", pending)
	}

	// 模拟云端恢复，手动 TriggerSync 立即清空退避并补推全部 Outbox
	failCloud = false
	afterSt, err := eng.TriggerSync(ctx, SyncDirectionBoth)
	if err != nil {
		t.Fatalf("TriggerSync after recovery failed: %v", err)
	}
	if afterSt.State != "idle" || !afterSt.CloudConnected || afterSt.OutboxPending != 0 || afterSt.ConsecutiveFailures != 0 {
		t.Fatalf("expected clean recovered status, got %+v", afterSt)
	}

	var dealCnt int64
	cloudDB.Model(&FreebieDeal{}).Where("id = ?", "deal-1").Count(&dealCnt)
	if dealCnt != 1 {
		t.Fatalf("expected deal-1 pushed to cloud after recovery, got %d", dealCnt)
	}
}
