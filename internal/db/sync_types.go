package db

import (
	"context"
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// SyncDirection 定义物化同步的触发方向。
type SyncDirection string

const (
	SyncDirectionBoth SyncDirection = "both"
	SyncDirectionPush SyncDirection = "push"
	SyncDirectionPull SyncDirection = "pull"
)

// SyncTombstoneRecord 存储云端逻辑删除墓碑，确保跨设备切换时能够准确回放本地硬删除。
type SyncTombstoneRecord struct {
	TableNameStr string    `gorm:"primaryKey;size:128;column:table_name"`
	PK           string    `gorm:"primaryKey;size:255;column:pk"`
	DeletedAt    time.Time `gorm:"index;column:deleted_at"`
}

func (SyncTombstoneRecord) TableName() string {
	return "_sync_tombstones"
}

// SyncJournalRecord 记录云端每次同步推送的逻辑时钟水位（synced_at），
// 确保离线延迟推送或存在时钟偏差的设备写入在增量拉取时不会因业务 updated_at 较旧而被遗漏。
type SyncJournalRecord struct {
	TableNameStr string    `gorm:"primaryKey;size:128;column:table_name"`
	PK           string    `gorm:"primaryKey;size:255;column:pk"`
	SyncedAt     time.Time `gorm:"index;column:synced_at"`
}

func (SyncJournalRecord) TableName() string {
	return "_sync_journal"
}

// SyncConfig 配置物化数据库异步双向同步引擎参数。
type SyncConfig struct {
	Enabled         bool
	LocalPath       string
	PushInterval    time.Duration
	PullInterval    time.Duration
	BatchSize       int
	Timeout         time.Duration
	OnRemoteChanged func(changedTables map[string]int)
	OnStatusChange  func(status SyncStatus)
}

// TableSyncStatus 记录单张业务表的物化同步明细指标。
type TableSyncStatus struct {
	TableName         string `json:"table_name"`
	LastPullAt        string `json:"last_pull_at,omitempty"`
	InitialReconciled bool   `json:"initial_reconciled"`
	PendingOutbox     int64  `json:"pending_outbox"`
	PushedRows        int64  `json:"pushed_rows"`
	PulledRows        int64  `json:"pulled_rows"`
	DeletedRows       int64  `json:"deleted_rows"`
	HasBlobData       bool   `json:"has_blob_data,omitempty"`
}

// SyncStatus 为物化数据库同步引擎的对外状态快照（所有连接串已脱敏）。
type SyncStatus struct {
	Enabled             bool              `json:"enabled"`
	Mode                string            `json:"mode"`  // materialized | direct | local_only
	State               string            `json:"state"` // idle | connecting | hydrating | syncing | degraded | disabled | stopped
	LocalPath           string            `json:"local_path,omitempty"`
	CloudTarget         string            `json:"cloud_target,omitempty"`
	CloudConnected      bool              `json:"cloud_connected"`
	InitialHydrated     bool              `json:"initial_hydrated"`
	OutboxPending       int64             `json:"outbox_pending"`
	LastPushAt          string            `json:"last_push_at,omitempty"`
	LastPullAt          string            `json:"last_pull_at,omitempty"`
	LastSyncLatencyMs   int64             `json:"last_sync_latency_ms"`
	TotalPushed         int64             `json:"total_pushed"`
	TotalPulled         int64             `json:"total_pulled"`
	TotalDeleted        int64             `json:"total_deleted"`
	ConsecutiveFailures int               `json:"consecutive_failures"`
	LastError           string            `json:"last_error,omitempty"`
	NextRetryAt         string            `json:"next_retry_at,omitempty"`
	PushInterval        string            `json:"push_interval,omitempty"`
	PullInterval        string            `json:"pull_interval,omitempty"`
	Tables              []TableSyncStatus `json:"tables,omitempty"`
}

// SyncRow 表示经过主键与 LWW 时间戳提取后的单行记录。
type SyncRow struct {
	PK        string
	Timestamp time.Time
	Record    any
}

// SyncTableOptions 允许覆盖自动解析的表元信息。
type SyncTableOptions struct {
	HasBlobData bool
}

// SyncTableSpec 为单张业务表的类型安全跨方言同步适配器。
type SyncTableSpec struct {
	TableName    string
	PKColumn     string
	UpdatedAtCol string
	CreatedAtCol string
	HasBlobData  bool
	NewModel     func() any
	ExtractRow   func(any) SyncRow
	FetchByPKs   func(ctx context.Context, db *gorm.DB, pks []string) ([]SyncRow, error)
	FetchAll     func(ctx context.Context, db *gorm.DB, limit, offset int) ([]SyncRow, error)
	FetchSince   func(ctx context.Context, db *gorm.DB, since time.Time, limit, offset int) ([]SyncRow, error)
	UpsertBatch  func(ctx context.Context, db *gorm.DB, rows []any, batchSize int) error
	DeleteByPKs  func(ctx context.Context, db *gorm.DB, pks []string) error
}

// NewTypedSyncTable 使用泛型模型 T 构造类型安全的同步表适配器，自动兼容 SQLite 与 PostgreSQL/MySQL。
func NewTypedSyncTable[T any](opts ...SyncTableOptions) SyncTableSpec {
	var opt SyncTableOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	var zero T
	sch, err := schema.Parse(&zero, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		panic(fmt.Sprintf("sync: failed to parse schema for %T: %v", zero, err))
	}

	tableName := sch.Table
	pkField := sch.PrioritizedPrimaryField
	if pkField == nil {
		panic(fmt.Sprintf("sync: table %q (%T) has no primary key", tableName, zero))
	}
	pkCol := pkField.DBName

	var updatedField *schema.Field
	if f := sch.LookUpField("updated_at"); f != nil {
		updatedField = f
	}
	var createdField *schema.Field
	if f := sch.LookUpField("created_at"); f != nil {
		createdField = f
	}
	hasBlob := opt.HasBlobData
	if !hasBlob && sch.LookUpField("BlobData") != nil {
		hasBlob = true
	}

	updatedCol := ""
	if updatedField != nil {
		updatedCol = updatedField.DBName
	}
	createdCol := ""
	if createdField != nil {
		createdCol = createdField.DBName
	}

	nonPKCols := make([]string, 0, len(sch.DBNames))
	for _, col := range sch.DBNames {
		if col != pkCol {
			nonPKCols = append(nonPKCols, col)
		}
	}

	extractRow := func(rec T) SyncRow {
		rv := reflect.ValueOf(rec)
		pkVal, _ := pkField.ValueOf(context.Background(), rv)
		pkStr := fmt.Sprint(pkVal)
		var ts time.Time
		if updatedField != nil {
			if v, zeroVal := updatedField.ValueOf(context.Background(), rv); !zeroVal {
				if t, ok := toUTCTime(v); ok {
					ts = t
				}
			}
		}
		if ts.IsZero() && createdField != nil {
			if v, zeroVal := createdField.ValueOf(context.Background(), rv); !zeroVal {
				if t, ok := toUTCTime(v); ok {
					ts = t
				}
			}
		}
		return SyncRow{
			PK:        pkStr,
			Timestamp: ts,
			Record:    rec,
		}
	}

	toSyncRows := func(items []T) []SyncRow {
		out := make([]SyncRow, 0, len(items))
		for _, item := range items {
			out = append(out, extractRow(item))
		}
		return out
	}

	toColumnMaps := func(ctx context.Context, rows []any) ([]map[string]any, error) {
		maps := make([]map[string]any, 0, len(rows))
		for _, raw := range rows {
			var rec T
			switch typed := raw.(type) {
			case T:
				rec = typed
			case *T:
				if typed == nil {
					continue
				}
				rec = *typed
			default:
				return nil, fmt.Errorf("sync(%s): unexpected record type %T", tableName, raw)
			}

			rv := reflect.ValueOf(rec)
			m := make(map[string]any, len(sch.DBNames))
			for _, dbCol := range sch.DBNames {
				f := sch.FieldsByDBName[dbCol]
				if f == nil {
					continue
				}
				val, _ := f.ValueOf(ctx, rv)
				if f.Serializer != nil {
					if dv, ok := val.(driver.Valuer); ok {
						serVal, serErr := dv.Value()
						if serErr != nil {
							return nil, fmt.Errorf("sync(%s): serialize field %s: %w", tableName, dbCol, serErr)
						}
						val = serVal
					}
				} else if val != nil {
					valRV := reflect.ValueOf(val)
					if valRV.Kind() == reflect.Ptr && valRV.IsNil() {
						val = nil
					}
				}
				m[dbCol] = val
			}
			maps = append(maps, m)
		}
		return maps, nil
	}

	return SyncTableSpec{
		TableName:    tableName,
		PKColumn:     pkCol,
		UpdatedAtCol: updatedCol,
		CreatedAtCol: createdCol,
		HasBlobData:  hasBlob,
		NewModel:     func() any { return new(T) },
		ExtractRow: func(raw any) SyncRow {
			switch typed := raw.(type) {
			case T:
				return extractRow(typed)
			case *T:
				if typed != nil {
					return extractRow(*typed)
				}
			}
			return SyncRow{}
		},
		FetchByPKs: func(ctx context.Context, db *gorm.DB, pks []string) ([]SyncRow, error) {
			if len(pks) == 0 {
				return nil, nil
			}
			out := make([]SyncRow, 0, len(pks))
			qPK := quoteCol(db, pkCol)
			for _, chunk := range chunkStrings(pks, 400) {
				var items []T
				if err := db.WithContext(ctx).Where(fmt.Sprintf("%s IN ?", qPK), chunk).Find(&items).Error; err != nil {
					return nil, err
				}
				out = append(out, toSyncRows(items)...)
			}
			return out, nil
		},
		FetchAll: func(ctx context.Context, db *gorm.DB, limit, offset int) ([]SyncRow, error) {
			var items []T
			q := db.WithContext(ctx).Order(fmt.Sprintf("%s ASC", quoteCol(db, pkCol)))
			if limit > 0 {
				q = q.Limit(limit).Offset(offset)
			}
			if err := q.Find(&items).Error; err != nil {
				return nil, err
			}
			return toSyncRows(items), nil
		},
		FetchSince: func(ctx context.Context, db *gorm.DB, since time.Time, limit, offset int) ([]SyncRow, error) {
			var items []T
			q := db.WithContext(ctx)
			sinceUTC := since.UTC()
			switch {
			case updatedCol != "" && createdCol != "":
				q = q.Where(
					fmt.Sprintf("(%s >= ? OR %s >= ?)", quoteCol(db, updatedCol), quoteCol(db, createdCol)),
					sinceUTC, sinceUTC,
				)
			case updatedCol != "":
				q = q.Where(fmt.Sprintf("%s >= ?", quoteCol(db, updatedCol)), sinceUTC)
			case createdCol != "":
				q = q.Where(fmt.Sprintf("%s >= ?", quoteCol(db, createdCol)), sinceUTC)
			}
			q = q.Order(fmt.Sprintf("%s ASC", quoteCol(db, pkCol)))
			if limit > 0 {
				q = q.Limit(limit).Offset(offset)
			}
			if err := q.Find(&items).Error; err != nil {
				return nil, err
			}
			return toSyncRows(items), nil
		},
		UpsertBatch: func(ctx context.Context, db *gorm.DB, rows []any, batchSize int) error {
			if len(rows) == 0 {
				return nil
			}
			if batchSize <= 0 {
				batchSize = 100
			}
			if hasBlob && batchSize > 10 {
				batchSize = 10
			}
			maps, err := toColumnMaps(ctx, rows)
			if err != nil {
				return err
			}
			if len(maps) == 0 {
				return nil
			}
			conflict := clause.OnConflict{
				Columns:   []clause.Column{{Name: pkCol}},
				DoUpdates: clause.AssignmentColumns(nonPKCols),
			}
			if len(nonPKCols) == 0 {
				conflict = clause.OnConflict{
					Columns:   []clause.Column{{Name: pkCol}},
					DoNothing: true,
				}
			}
			for start := 0; start < len(maps); start += batchSize {
				end := start + batchSize
				if end > len(maps) {
					end = len(maps)
				}
				chunk := maps[start:end]
				if err := db.WithContext(ctx).Model(&zero).Clauses(conflict).Create(&chunk).Error; err != nil {
					if len(chunk) == 1 {
						return err
					}
					// 若批次中单条记录命中唯一约束冲突，逐条重试以免整批失败
					for i := range chunk {
						single := chunk[i : i+1]
						if sErr := db.WithContext(ctx).Model(&zero).Clauses(conflict).Create(&single).Error; sErr != nil {
							return sErr
						}
					}
				}
			}
			return nil
		},
		DeleteByPKs: func(ctx context.Context, db *gorm.DB, pks []string) error {
			if len(pks) == 0 {
				return nil
			}
			qPK := quoteCol(db, pkCol)
			for _, chunk := range chunkStrings(pks, 400) {
				if err := db.WithContext(ctx).Where(fmt.Sprintf("%s IN ?", qPK), chunk).Delete(&zero).Error; err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// DefaultSyncTables 返回 internal/db 中全部 18 张核心业务表的同步适配器。
func DefaultSyncTables() []SyncTableSpec {
	return []SyncTableSpec{
		NewTypedSyncTable[GalleryWork](SyncTableOptions{HasBlobData: true}),
		NewTypedSyncTable[Conversation](),
		NewTypedSyncTable[ChatMessageRecord](),
		NewTypedSyncTable[ChatPreset](),
		NewTypedSyncTable[PromptFavorite](),
		NewTypedSyncTable[VideoTaskRecord](),
		NewTypedSyncTable[AccountRuntimeState](),
		NewTypedSyncTable[PoolAccountRecord](),
		NewTypedSyncTable[ProxyNodeRecord](),
		NewTypedSyncTable[AccountProfileProxyRecord](),
		NewTypedSyncTable[AccountRestrictionRecord](),
		NewTypedSyncTable[CustomVoiceModelRecord](),
		NewTypedSyncTable[ProviderChannelRecord](),
		NewTypedSyncTable[CredentialFormatPatternRecord](),
		NewTypedSyncTable[AudioGenerationRecord](SyncTableOptions{HasBlobData: true}),
		NewTypedSyncTable[ProviderAccountRecord](),
		NewTypedSyncTable[DramaProjectRecord](),
		NewTypedSyncTable[DramaRunRecord](),
		NewTypedSyncTable[FreebieDeal](),
	}
}

func quoteCol(db *gorm.DB, col string) string {
	if db != nil && db.Dialector != nil {
		var b strings.Builder
		db.Dialector.QuoteTo(&b, col)
		return b.String()
	}
	return `"` + strings.ReplaceAll(col, `"`, `""`) + `"`
}

func toUTCTime(v any) (time.Time, bool) {
	switch tv := v.(type) {
	case time.Time:
		if tv.IsZero() {
			return time.Time{}, false
		}
		return tv.UTC(), true
	case *time.Time:
		if tv == nil || tv.IsZero() {
			return time.Time{}, false
		}
		return tv.UTC(), true
	default:
		return time.Time{}, false
	}
}

func chunkStrings(items []string, size int) [][]string {
	if size <= 0 || len(items) <= size {
		if len(items) == 0 {
			return nil
		}
		return [][]string{items}
	}
	chunks := make([][]string, 0, (len(items)+size-1)/size)
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[i:end])
	}
	return chunks
}
