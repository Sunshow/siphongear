package apikey

import (
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/store/models"
)

const (
	tokenPrefix    = "sg_"
	prefixLen      = 8
	secretLen      = 32
	bcryptCost     = 10
	lastUsedThrott = time.Minute
)

// alphabet excludes 0/1/I/O for human readability.
const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// Generated represents a freshly generated key with its plaintext.
// Plaintext is only ever exposed at creation/rotation time.
type Generated struct {
	Prefix     string
	Secret     string
	SecretHash string
	Plaintext  string
}

// Generate creates a brand new (prefix, secret, hash, plaintext) tuple.
func Generate() (*Generated, error) {
	prefix, err := randomString(prefixLen)
	if err != nil {
		return nil, err
	}
	secret, err := randomString(secretLen)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcryptCost)
	if err != nil {
		return nil, err
	}
	return &Generated{
		Prefix:     prefix,
		Secret:     secret,
		SecretHash: string(hash),
		Plaintext:  tokenPrefix + prefix + "_" + secret,
	}, nil
}

func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}

// Parse splits a token into (prefix, secret).
func Parse(token string) (prefix, secret string, ok bool) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, tokenPrefix) {
		return "", "", false
	}
	rest := token[len(tokenPrefix):]
	idx := strings.Index(rest, "_")
	if idx <= 0 || idx == len(rest)-1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}

// MaskedPrefix renders a prefix for UI display: "sg_AB3KXXXX…" -> "sg_AB3K…"
// We just expose the raw prefix; this helper is for completeness.
func MaskedPrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	return tokenPrefix + prefix + "..."
}

// Verifier wraps DB lookup + bcrypt verification + throttled last_used_at writes.
type Verifier struct {
	db        *gorm.DB
	mu        sync.Mutex
	lastWrite map[uint]time.Time
}

func NewVerifier(db *gorm.DB) *Verifier {
	return &Verifier{db: db, lastWrite: map[uint]time.Time{}}
}

var (
	ErrInvalid  = errors.New("invalid api key")
	ErrDisabled = errors.New("api key disabled")
)

// Verify resolves a token to a usable APIKey row, or returns ErrInvalid / ErrDisabled.
// Side effect: schedules a throttled async update of LastUsedAt.
func (v *Verifier) Verify(token string) (*models.APIKey, error) {
	prefix, secret, ok := Parse(token)
	if !ok {
		return nil, ErrInvalid
	}
	var row models.APIKey
	if err := v.db.Where("prefix = ?", prefix).First(&row).Error; err != nil {
		return nil, ErrInvalid
	}
	if err := bcrypt.CompareHashAndPassword([]byte(row.SecretHash), []byte(secret)); err != nil {
		return nil, ErrInvalid
	}
	if !row.Enabled {
		return nil, ErrDisabled
	}
	v.touch(row.ID)
	return &row, nil
}

func (v *Verifier) touch(id uint) {
	now := time.Now()
	v.mu.Lock()
	last := v.lastWrite[id]
	if now.Sub(last) < lastUsedThrott {
		v.mu.Unlock()
		return
	}
	v.lastWrite[id] = now
	v.mu.Unlock()
	go func() {
		_ = v.db.Model(&models.APIKey{}).
			Where("id = ?", id).
			Update("last_used_at", now).Error
	}()
}
