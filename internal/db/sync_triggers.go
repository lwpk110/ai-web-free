package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type outboxItem struct {
	Seq       int64  `gorm:"column:seq"`
	TableName string `gorm:"column:table_name"`
	PK        string `gorm:"column:pk"`
	Op        string `gorm:"column:op"`
	ChangedAt string `gorm:"column:changed_at"`
}

type syncStateRow struct {
	TableName         string `gorm:"column:table_name"`
	LastPullAt        string `gorm:"column:last_pull_at"`
	InitialReconciled int    `gorm:"column:initial_reconciled"`
	UpdatedAt         string `gorm:"column:updated_at"`
}

// initLocalSyncMetadataAndTriggers 在本地 SQLite 初始化物化同步元数据表与全部业务表的事务级变更捕获触发器。
func initLocalSyncMetadataAndTriggers(localDB *gorm.DB, tables []SyncTableSpec) error {
	if localDB == nil {
		return fmt.Errorf("localDB is nil")
	}
	if sqlDB, err := localDB.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}

	ddl := []string{
		`CREATE TABLE IF NOT EXISTS _sync_control (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			suppress_triggers INTEGER NOT NULL DEFAULT 0
		)`,
		`INSERT OR IGNORE INTO _sync_control (id, suppress_triggers) VALUES (1, 0)`,
		`UPDATE _sync_control SET suppress_triggers = 0 WHERE id = 1`,
		`CREATE TABLE IF NOT EXISTS _sync_outbox (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			table_name TEXT NOT NULL,
			pk TEXT NOT NULL,
			op TEXT NOT NULL,
			changed_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_outbox_table_pk ON _sync_outbox(table_name, pk)`,
		`CREATE TABLE IF NOT EXISTS _sync_state (
			table_name TEXT PRIMARY KEY,
			last_pull_at TEXT NOT NULL DEFAULT '',
			initial_reconciled INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, stmt := range ddl {
		if err := localDB.Exec(stmt).Error; err != nil {
			return fmt.Errorf("init local sync metadata (%s): %w", stmt, err)
		}
	}

	for _, tbl := range tables {
		if err := installTableTriggers(localDB, tbl); err != nil {
			return err
		}
	}
	return nil
}

func installTableTriggers(localDB *gorm.DB, tbl SyncTableSpec) error {
	safeTable := strings.ReplaceAll(tbl.TableName, `"`, `""`)
	safePK := strings.ReplaceAll(tbl.PKColumn, `"`, `""`)
	escTableLiteral := strings.ReplaceAll(tbl.TableName, `'`, `''`)

	trigNames := map[string]string{
		"ai": fmt.Sprintf("trg_sync_%s_ai", tbl.TableName),
		"au": fmt.Sprintf("trg_sync_%s_au", tbl.TableName),
		"ad": fmt.Sprintf("trg_sync_%s_ad", tbl.TableName),
	}

	for _, trigName := range trigNames {
		dropSQL := fmt.Sprintf(`DROP TRIGGER IF EXISTS "%s"`, strings.ReplaceAll(trigName, `"`, `""`))
		if err := localDB.Exec(dropSQL).Error; err != nil {
			return fmt.Errorf("drop trigger %s: %w", trigName, err)
		}
	}

	createStmts := []string{
		fmt.Sprintf(`CREATE TRIGGER "%s"
AFTER INSERT ON "%s"
WHEN COALESCE((SELECT suppress_triggers FROM _sync_control WHERE id = 1), 0) = 0
BEGIN
	INSERT INTO _sync_outbox (table_name, pk, op, changed_at)
	VALUES ('%s', CAST(NEW."%s" AS TEXT), 'upsert', strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ', 'now'));
END`, strings.ReplaceAll(trigNames["ai"], `"`, `""`), safeTable, escTableLiteral, safePK),

		fmt.Sprintf(`CREATE TRIGGER "%s"
AFTER UPDATE ON "%s"
WHEN COALESCE((SELECT suppress_triggers FROM _sync_control WHERE id = 1), 0) = 0
BEGIN
	INSERT INTO _sync_outbox (table_name, pk, op, changed_at)
	VALUES ('%s', CAST(NEW."%s" AS TEXT), 'upsert', strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ', 'now'));
END`, strings.ReplaceAll(trigNames["au"], `"`, `""`), safeTable, escTableLiteral, safePK),

		fmt.Sprintf(`CREATE TRIGGER "%s"
AFTER DELETE ON "%s"
WHEN COALESCE((SELECT suppress_triggers FROM _sync_control WHERE id = 1), 0) = 0
BEGIN
	INSERT INTO _sync_outbox (table_name, pk, op, changed_at)
	VALUES ('%s', CAST(OLD."%s" AS TEXT), 'delete', strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ', 'now'));
END`, strings.ReplaceAll(trigNames["ad"], `"`, `""`), safeTable, escTableLiteral, safePK),
	}

	for _, sqlText := range createStmts {
		if err := localDB.Exec(sqlText).Error; err != nil {
			return fmt.Errorf("create trigger on %s: %w", tbl.TableName, err)
		}
	}
	return nil
}

// withSuppressedTriggers 在单一 SQLite 写事务内临时抑制触发器，防止云端拉取回写再次产生 Outbox 回环。
func withSuppressedTriggers(localDB *gorm.DB, fn func(tx *gorm.DB) error) error {
	err := localDB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE _sync_control SET suppress_triggers = 1 WHERE id = 1`).Error; err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			return err
		}
		return tx.Exec(`UPDATE _sync_control SET suppress_triggers = 0 WHERE id = 1`).Error
	})
	if err != nil {
		_ = localDB.Exec(`UPDATE _sync_control SET suppress_triggers = 0 WHERE id = 1`).Error
	}
	return err
}

func parseOutboxTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	return time.Time{}
}
