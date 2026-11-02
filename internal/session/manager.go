package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/crypto"
	"github.com/sunshow/siphongear/internal/store/models"
)

// ErrNotFound is returned by Delete when the session does not exist.
var ErrNotFound = errors.New("session not found")

// Info is the public (secret-free) view of a persisted session.
type Info struct {
	ID          uint       `json:"id"`
	Key         string     `json:"key"`
	CollectorID uint       `json:"collector_id"`
	CookieCount int        `json:"cookie_count"`
	VarKeys     string     `json:"var_keys"`
	LoginCount  int        `json:"login_count"`
	LastLoginAt *time.Time `json:"last_login_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// ListFilter narrows List results.
type ListFilter struct {
	CollectorID uint
	SiteID      uint
}

// Manager stores session state encrypted in the sessions table. Values are
// encrypted with the same AES-GCM key as credentials; concurrent access is
// last-write-wins per key (the runner serialises runs of a single collector).
type Manager struct {
	db     *gorm.DB
	cipher *crypto.Cipher
}

// NewManager builds a Manager on top of the given DB and cipher.
func NewManager(db *gorm.DB, cipher *crypto.Cipher) *Manager {
	return &Manager{db: db, cipher: cipher}
}

// Load returns the decrypted state for key, or (nil, nil) when absent.
func (m *Manager) Load(ctx context.Context, key string) (*State, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	key = normalizeKey(key)
	if key == "" {
		return nil, fmt.Errorf("session key is empty")
	}
	var row models.Session
	if err := m.db.WithContext(ctx).Where(map[string]any{"session_key": key}).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("load session %q: %w", key, err)
	}
	state, err := m.decrypt(row.Payload)
	if err != nil {
		return nil, fmt.Errorf("decrypt session %q: %w", key, err)
	}
	state.CollectorID = row.CollectorID
	if state.CreatedAt.IsZero() {
		state.CreatedAt = row.CreatedAt
	}
	return state, nil
}

// Save encrypts and upserts the state for key. Empty states remove the row so a
// fully expired/cleared session does not linger.
func (m *Manager) Save(ctx context.Context, key string, state *State) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("session store is not configured")
	}
	key = normalizeKey(key)
	if key == "" {
		return fmt.Errorf("session key is empty")
	}
	if state == nil || !state.HasData() {
		return m.deleteKey(ctx, key)
	}
	now := time.Now()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
	state.UpdatedAt = now
	if state.Vars == nil {
		state.Vars = map[string]any{}
	}
	payload, err := m.encrypt(state)
	if err != nil {
		return fmt.Errorf("encrypt session %q: %w", key, err)
	}
	cookieCount := state.CookieCount(now)
	varKeys := joinKeys(state.VarKeys())

	var row models.Session
	err = m.db.WithContext(ctx).Where(map[string]any{"session_key": key}).First(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		row = models.Session{
			SessionKey:  key,
			CollectorID: state.CollectorID,
			Payload:     payload,
			CookieCount: cookieCount,
			VarKeys:     varKeys,
			LoginCount:  state.LoginCount,
			LastLoginAt: state.LastLoginAt,
			LastUsedAt:  &now,
			ExpiresAt:   state.ExpiresAt,
			CreatedAt:   state.CreatedAt,
			UpdatedAt:   now,
		}
		return m.db.WithContext(ctx).Create(&row).Error
	case err != nil:
		return fmt.Errorf("load session %q: %w", key, err)
	default:
		return m.db.WithContext(ctx).Model(&models.Session{}).Where("id = ?", row.ID).Updates(map[string]any{
			"collector_id":  state.CollectorID,
			"payload":       payload,
			"cookie_count":  cookieCount,
			"var_keys":      varKeys,
			"login_count":   state.LoginCount,
			"last_login_at": state.LastLoginAt,
			"last_used_at":  now,
			"expires_at":    state.ExpiresAt,
			"updated_at":    now,
		}).Error
	}
}

// Delete removes one session by database ID.
func (m *Manager) Delete(ctx context.Context, id uint) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("session store is not configured")
	}
	res := m.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Session{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteByCollector removes every session owned by a collector.
func (m *Manager) DeleteByCollector(ctx context.Context, collectorID uint) (int64, error) {
	if m == nil || m.db == nil {
		return 0, fmt.Errorf("session store is not configured")
	}
	res := m.db.WithContext(ctx).Where("collector_id = ?", collectorID).Delete(&models.Session{})
	return res.RowsAffected, res.Error
}

// List returns secret-free session metadata, most recently used first.
func (m *Manager) List(ctx context.Context, filter ListFilter) ([]Info, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	q := m.db.WithContext(ctx).Model(&models.Session{})
	if filter.SiteID != 0 {
		q = q.Joins("JOIN collectors ON collectors.id = sessions.collector_id").
			Where("collectors.site_id = ?", filter.SiteID)
	}
	if filter.CollectorID != 0 {
		q = q.Where("sessions.collector_id = ?", filter.CollectorID)
	}
	var rows []models.Session
	if err := q.Order("sessions.updated_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]Info, 0, len(rows))
	for _, row := range rows {
		status := "expired"
		if (row.CookieCount > 0 || row.VarKeys != "") && (row.ExpiresAt == nil || row.ExpiresAt.After(now)) {
			status = "active"
		}
		out = append(out, Info{
			ID:          row.ID,
			Key:         row.SessionKey,
			CollectorID: row.CollectorID,
			CookieCount: row.CookieCount,
			VarKeys:     row.VarKeys,
			LoginCount:  row.LoginCount,
			LastLoginAt: row.LastLoginAt,
			LastUsedAt:  row.LastUsedAt,
			ExpiresAt:   row.ExpiresAt,
			Status:      status,
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
		})
	}
	return out, nil
}

func (m *Manager) deleteKey(ctx context.Context, key string) error {
	return m.db.WithContext(ctx).Where(map[string]any{"session_key": key}).Delete(&models.Session{}).Error
}

func (m *Manager) encrypt(state *State) (string, error) {
	plain, err := sonic.Marshal(state)
	if err != nil {
		return "", err
	}
	if m.cipher == nil {
		return string(plain), nil
	}
	return m.cipher.EncryptString(string(plain))
}

func (m *Manager) decrypt(payload string) (*State, error) {
	if payload == "" {
		return NewState(), nil
	}
	plain := payload
	if m.cipher != nil {
		decoded, err := m.cipher.DecryptString(payload)
		if err != nil {
			return nil, err
		}
		plain = decoded
	}
	state := NewState()
	if err := sonic.UnmarshalString(plain, state); err != nil {
		return nil, err
	}
	if state.Vars == nil {
		state.Vars = map[string]any{}
	}
	return state, nil
}

// normalizeKey trims and bounds a session key so it always fits the DB index.
func normalizeKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) > 191 {
		key = key[:191]
	}
	return key
}

func joinKeys(keys []string) string {
	out := strings.Join(keys, ",")
	if len(out) > 255 {
		out = out[:255]
	}
	return out
}
