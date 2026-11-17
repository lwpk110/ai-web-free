// Package gatewaykeys 提供网关 API Key 与账号分组（Sub2API 风格）：
// 每个 Agent 一把 Key，Key 绑定主分组（可选降级分组），分组决定可用账号、模型白名单与别名。
// 纯 Go 实现，GORM 持久化可选（db 为 nil 时仅内存）。
package gatewaykeys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// KeyPrefix 网关 API Key 前缀，用于与 ChatGPT 令牌 / 全局密钥区分。
const KeyPrefix = "sk-gw-"

var (
	ErrNotFound         = errors.New("not found")
	ErrRevealUnsupport  = errors.New("legacy key cannot be revealed; recreate this key")
	ErrRateLimited      = errors.New("rate limit exceeded for this API key")
	ErrConcurrencyLimit = errors.New("concurrency limit exceeded for this API key")
	ErrModelNotAllowed  = errors.New("model is not allowed for this API key group")
	ErrInvalid          = errors.New("invalid parameter")
)

// Group 账号分组。AccountIDs 非空时仅这些账号可用；否则按 AccountTypes 筛选（默认 free）。
type Group struct {
	ID              string            `gorm:"primaryKey;size:64" json:"id"`
	Name            string            `gorm:"size:128;uniqueIndex" json:"name"`
	Provider        string            `gorm:"size:32;default:'chatgpt'" json:"provider"`
	AccountTypes    []string          `gorm:"serializer:json;type:text" json:"account_types"`
	AccountIDs      []string          `gorm:"serializer:json;type:text" json:"account_ids"`
	ModelAllowlist  []string          `gorm:"serializer:json;type:text" json:"model_allowlist"`
	ModelAliases    map[string]string `gorm:"serializer:json;type:text" json:"model_aliases"`
	FallbackGroupID string            `gorm:"size:64" json:"fallback_group_id,omitempty"`
	Enabled         bool              `gorm:"default:true" json:"enabled"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// TableName 避免与其它包的 groups 表冲突。
func (Group) TableName() string { return "gateway_groups" }

// Key 网关 API Key。KeyHash 用于鉴权，KeyCipher 为 AES-GCM 密文（支持后续复制显示）。
type Key struct {
	ID             string     `gorm:"primaryKey;size:64" json:"id"`
	Name           string     `gorm:"size:128" json:"name"`
	KeyHash        string     `gorm:"size:64;uniqueIndex" json:"-"`
	KeyCipher      string     `gorm:"type:text" json:"-"`
	Prefix         string     `gorm:"size:24" json:"prefix"`
	GroupID        string     `gorm:"size:64;index" json:"group_id"`
	RPM            int        `json:"rpm"`
	MaxConcurrent  int        `json:"max_concurrent"`
	Enabled        bool       `gorm:"default:true" json:"enabled"`
	TotalRequests  int64      `json:"total_requests"`
	FailedRequests int64      `json:"failed_requests"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (Key) TableName() string { return "gateway_keys" }

// Store Key/分组存储，内存为准，写操作同步落库。
type Store struct {
	mu     sync.RWMutex
	db     *gorm.DB
	aead   cipher.AEAD
	groups map[string]*Group
	keys   map[string]*Key // by ID
	byHash map[string]string
	limits map[string]*limiter
}

// NewStore 创建存储；secret 为空时直接保存可重复显示的 Key 明文。db 可为 nil。
func NewStore(db *gorm.DB, secret string) (*Store, error) {
	s := &Store{
		db:     db,
		groups: map[string]*Group{},
		keys:   map[string]*Key{},
		byHash: map[string]string{},
		limits: map[string]*limiter{},
	}
	if secret = strings.TrimSpace(secret); secret != "" {
		sum := sha256.Sum256([]byte("ai-web-free/gateway-keys/" + secret))
		block, err := aes.NewCipher(sum[:])
		if err != nil {
			return nil, err
		}
		if s.aead, err = cipher.NewGCM(block); err != nil {
			return nil, err
		}
	}
	if db != nil {
		if err := db.AutoMigrate(&Group{}, &Key{}); err != nil {
			return nil, err
		}
		var gs []*Group
		var ks []*Key
		if err := db.Find(&gs).Error; err != nil {
			return nil, err
		}
		if err := db.Find(&ks).Error; err != nil {
			return nil, err
		}
		for _, g := range gs {
			s.groups[g.ID] = g
		}
		for _, k := range ks {
			s.keys[k.ID] = k
			s.byHash[k.KeyHash] = k.ID
		}
	}
	return s, nil
}

func hashKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func (s *Store) encrypt(plain string) string {
	if s.aead == nil {
		return plain
	}
	nonce := make([]byte, s.aead.NonceSize())
	_, _ = rand.Read(nonce)
	return base64.StdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(plain), nil))
}

func (s *Store) decrypt(enc string) (string, error) {
	if enc == "" {
		return "", ErrRevealUnsupport
	}
	if s.aead == nil || strings.HasPrefix(enc, KeyPrefix) {
		return enc, nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || len(raw) < s.aead.NonceSize() {
		return "", ErrRevealUnsupport
	}
	n := s.aead.NonceSize()
	out, err := s.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", ErrRevealUnsupport
	}
	return string(out), nil
}

// HasKeys 是否已配置任一网关 Key。
func (s *Store) HasKeys() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.keys) > 0
}

// IsGatewayKey 判断令牌形态是否为网关 Key。
func IsGatewayKey(token string) bool { return strings.HasPrefix(token, KeyPrefix) }

// ─── Group CRUD ──────────────────────────────────────────────

func normalizeGroup(g *Group) error {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" {
		return fmt.Errorf("%w: name required", ErrInvalid)
	}
	if g.Provider == "" {
		g.Provider = "chatgpt"
	}
	for i, t := range g.AccountTypes {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "plus" || t == "paid" {
			t = "puid"
		}
		if t != "free" && t != "puid" && t != "noauth" {
			return fmt.Errorf("%w: account type %q", ErrInvalid, t)
		}
		g.AccountTypes[i] = t
	}
	return nil
}

func (s *Store) CreateGroup(g Group) (*Group, error) {
	if err := normalizeGroup(&g); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.groups {
		if e.Name == g.Name {
			return nil, fmt.Errorf("%w: duplicate group name", ErrInvalid)
		}
	}
	g.ID = uuid.NewString()
	g.CreatedAt, g.UpdatedAt = time.Now(), time.Now()
	if s.db != nil {
		if err := s.db.Create(&g).Error; err != nil {
			return nil, err
		}
	}
	s.groups[g.ID] = &g
	cp := g
	return &cp, nil
}

func (s *Store) UpdateGroup(id string, in Group) (*Group, error) {
	if err := normalizeGroup(&in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.groups[id]
	if !ok {
		return nil, ErrNotFound
	}
	for _, e := range s.groups {
		if e.ID != id && e.Name == in.Name {
			return nil, fmt.Errorf("%w: duplicate group name", ErrInvalid)
		}
	}
	if in.FallbackGroupID == id {
		return nil, fmt.Errorf("%w: fallback cannot be self", ErrInvalid)
	}
	in.ID, in.CreatedAt, in.UpdatedAt = id, cur.CreatedAt, time.Now()
	if s.db != nil {
		if err := s.db.Save(&in).Error; err != nil {
			return nil, err
		}
	}
	s.groups[id] = &in
	cp := in
	return &cp, nil
}

func (s *Store) DeleteGroup(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.groups[id]; !ok {
		return ErrNotFound
	}
	for _, k := range s.keys {
		if k.GroupID == id {
			return fmt.Errorf("%w: group still bound by key %q", ErrInvalid, k.Name)
		}
	}
	if s.db != nil {
		if err := s.db.Delete(&Group{}, "id = ?", id).Error; err != nil {
			return err
		}
	}
	delete(s.groups, id)
	return nil
}

func (s *Store) ListGroups() []Group {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Group, 0, len(s.groups))
	for _, g := range s.groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) GetGroup(id string) (*Group, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.groups[id]
	if !ok {
		return nil, false
	}
	cp := *g
	return &cp, true
}

// ─── Key CRUD ────────────────────────────────────────────────

// CreateKey 生成新 Key，返回记录与明文。
func (s *Store) CreateKey(name, groupID string, rpm, maxConc int) (*Key, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || rpm < 0 || maxConc < 0 {
		return nil, "", ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.groups[groupID]; !ok {
		return nil, "", fmt.Errorf("%w: group not found", ErrInvalid)
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return nil, "", err
	}
	plain := KeyPrefix + hex.EncodeToString(buf)
	k := &Key{
		ID: uuid.NewString(), Name: name, KeyHash: hashKey(plain), KeyCipher: s.encrypt(plain),
		Prefix: plain[:len(KeyPrefix)+4] + "…", GroupID: groupID, RPM: rpm, MaxConcurrent: maxConc,
		Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if s.db != nil {
		if err := s.db.Create(k).Error; err != nil {
			return nil, "", err
		}
	}
	s.keys[k.ID] = k
	s.byHash[k.KeyHash] = k.ID
	cp := *k
	return &cp, plain, nil
}

// UpdateKey 更新名称、分组、限额与启用状态。
func (s *Store) UpdateKey(id, name, groupID string, rpm, maxConc int, enabled bool) (*Key, error) {
	if rpm < 0 || maxConc < 0 {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[id]
	if !ok {
		return nil, ErrNotFound
	}
	if _, ok := s.groups[groupID]; !ok {
		return nil, fmt.Errorf("%w: group not found", ErrInvalid)
	}
	if n := strings.TrimSpace(name); n != "" {
		k.Name = n
	}
	k.GroupID, k.RPM, k.MaxConcurrent, k.Enabled, k.UpdatedAt = groupID, rpm, maxConc, enabled, time.Now()
	if s.db != nil {
		if err := s.db.Save(k).Error; err != nil {
			return nil, err
		}
	}
	cp := *k
	return &cp, nil
}

func (s *Store) DeleteKey(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[id]
	if !ok {
		return ErrNotFound
	}
	if s.db != nil {
		if err := s.db.Delete(&Key{}, "id = ?", id).Error; err != nil {
			return err
		}
	}
	delete(s.byHash, k.KeyHash)
	delete(s.keys, id)
	delete(s.limits, id)
	return nil
}

// RevealKey 解密返回 Key 明文。
func (s *Store) RevealKey(id string) (string, error) {
	s.mu.RLock()
	k, ok := s.keys[id]
	s.mu.RUnlock()
	if !ok {
		return "", ErrNotFound
	}
	return s.decrypt(k.KeyCipher)
}

func (s *Store) ListKeys() []Key {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Key, 0, len(s.keys))
	for _, k := range s.keys {
		out = append(out, *k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// ─── 鉴权 / 限流 / 统计 ─────────────────────────────────────

// Authenticate 校验令牌，成功返回 Key 副本与其主分组。
func (s *Store) Authenticate(token string) (*Key, *Group, error) {
	h := hashKey(token)
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byHash[h]
	if !ok {
		return nil, nil, ErrNotFound
	}
	k := s.keys[id]
	if subtle.ConstantTimeCompare([]byte(k.KeyHash), []byte(h)) != 1 || !k.Enabled {
		return nil, nil, ErrNotFound
	}
	g, ok := s.groups[k.GroupID]
	if !ok || !g.Enabled {
		return nil, nil, fmt.Errorf("%w: group disabled or missing", ErrNotFound)
	}
	kc, gc := *k, *g
	return &kc, &gc, nil
}

// Admit 通过限流与并发检查，返回结束时需调用的 release。
func (s *Store) Admit(k *Key) (release func(), err error) {
	s.mu.Lock()
	l := s.limits[k.ID]
	if l == nil {
		l = &limiter{}
		s.limits[k.ID] = l
	}
	s.mu.Unlock()
	return l.admit(k.RPM, k.MaxConcurrent, time.Now())
}

// Record 记录一次请求结果。
func (s *Store) Record(keyID string, failed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[keyID]
	if !ok {
		return
	}
	now := time.Now()
	k.TotalRequests++
	if failed {
		k.FailedRequests++
	}
	k.LastUsedAt = &now
	// 统计为尽力而为：不阻塞请求，仅每 20 次落库一次。
	if s.db != nil && k.TotalRequests%20 == 0 {
		cp := *k
		go s.db.Model(&Key{}).Where("id = ?", cp.ID).Updates(map[string]any{
			"total_requests": cp.TotalRequests, "failed_requests": cp.FailedRequests, "last_used_at": cp.LastUsedAt,
		})
	}
}

// Fallback 返回分组的降级分组（启用且存在时）。
func (s *Store) Fallback(g *Group) *Group {
	if g == nil || g.FallbackGroupID == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	fb, ok := s.groups[g.FallbackGroupID]
	if !ok || !fb.Enabled {
		return nil
	}
	cp := *fb
	return &cp
}

// ─── 分组 → 账号匹配 / 模型 ──────────────────────────────────

// Types 返回分组需要遍历的账号类型（按优先级）。
func (g *Group) Types() []accounts.AccountType {
	if len(g.AccountIDs) > 0 {
		return []accounts.AccountType{accounts.TypeFree, accounts.TypePUID, accounts.TypeNoAuth}
	}
	names := g.AccountTypes
	if len(names) == 0 {
		names = []string{"free"}
	}
	out := make([]accounts.AccountType, 0, len(names))
	for _, n := range names {
		switch n {
		case "free":
			out = append(out, accounts.TypeFree)
		case "puid":
			out = append(out, accounts.TypePUID)
		case "noauth":
			out = append(out, accounts.TypeNoAuth)
		}
	}
	return out
}

// Match 返回账号是否属于该分组。
func (g *Group) Match() func(*accounts.Account) bool {
	if len(g.AccountIDs) == 0 {
		return func(*accounts.Account) bool { return true } // 类型已由 Types() 限定
	}
	set := make(map[string]bool, len(g.AccountIDs))
	for _, id := range g.AccountIDs {
		set[id] = true
	}
	return func(a *accounts.Account) bool { return set[a.ID] }
}

// ResolveModel 校验请求模型并应用别名；返回上游实际模型名。
// 白名单与别名均为空时放行；空模型名放行（交由上游使用默认）。
func (g *Group) ResolveModel(requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" || (len(g.ModelAllowlist) == 0 && len(g.ModelAliases) == 0) {
		return requested, nil
	}
	allowed := false
	for _, m := range g.ModelAllowlist {
		if m == requested {
			allowed = true
			break
		}
	}
	target, aliased := g.ModelAliases[requested]
	if !allowed && !aliased {
		return "", fmt.Errorf("%w: %s", ErrModelNotAllowed, requested)
	}
	if aliased && strings.TrimSpace(target) != "" {
		return strings.TrimSpace(target), nil
	}
	return requested, nil
}

// VisibleModels 计算 /v1/models 对该分组可见的模型：
// 账号缓存模型（有白名单则取交集）∪ 别名名称。
func (g *Group) VisibleModels(accountModels []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(m string) {
		if m = strings.TrimSpace(m); m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	if len(g.ModelAllowlist) == 0 {
		for _, m := range accountModels {
			add(m)
		}
	} else {
		have := map[string]bool{}
		for _, m := range accountModels {
			have[m] = true
		}
		for _, m := range g.ModelAllowlist {
			if len(accountModels) == 0 || have[m] {
				add(m)
			}
		}
	}
	aliases := make([]string, 0, len(g.ModelAliases))
	for a := range g.ModelAliases {
		aliases = append(aliases, a)
	}
	sort.Strings(aliases)
	for _, a := range aliases {
		add(a)
	}
	return out
}

// ─── 限流器 ──────────────────────────────────────────────────

type limiter struct {
	mu     sync.Mutex
	stamps []time.Time
	active int
}

func (l *limiter) admit(rpm, maxConc int, now time.Time) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rpm > 0 {
		cut := now.Add(-time.Minute)
		i := 0
		for i < len(l.stamps) && l.stamps[i].Before(cut) {
			i++
		}
		l.stamps = l.stamps[i:]
		if len(l.stamps) >= rpm {
			return nil, ErrRateLimited
		}
	}
	if maxConc > 0 && l.active >= maxConc {
		return nil, ErrConcurrencyLimit
	}
	if rpm > 0 {
		l.stamps = append(l.stamps, now)
	}
	l.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.active--
			l.mu.Unlock()
		})
	}, nil
}
