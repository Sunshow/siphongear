package templates

import (
	"errors"
	"fmt"
	"sort"

	"github.com/bytedance/sonic"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/store/models"
)

// ErrConflictBuiltin indicates that the requested name conflicts with a builtin template.
var ErrConflictBuiltin = errors.New("template name conflicts with a builtin template")

// ErrNotFound indicates the template is not registered or stored.
var ErrNotFound = errors.New("template not found")

// Store provides CRUD over user-defined templates persisted in DB.
type Store struct {
	DB *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{DB: db}
}

func (s *Store) ListUser() ([]Template, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	var rows []models.CollectorTemplate
	if err := s.DB.Order("name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Template, 0, len(rows))
	for _, r := range rows {
		t, err := decode(r.Spec)
		if err != nil {
			continue
		}
		t.Name = r.Name
		t.Description = r.Description
		t.Source = "user"
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) GetUser(name string) (Template, bool, error) {
	if s == nil || s.DB == nil {
		return Template{}, false, nil
	}
	var row models.CollectorTemplate
	if err := s.DB.Where("name = ?", name).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Template{}, false, nil
		}
		return Template{}, false, err
	}
	t, err := decode(row.Spec)
	if err != nil {
		return Template{}, true, err
	}
	t.Name = row.Name
	t.Description = row.Description
	t.Source = "user"
	return t, true, nil
}

// ListAll merges builtin and user templates; user entries are skipped if their name
// collides with a builtin (builtin always wins).
func (s *Store) ListAll() ([]Template, error) {
	out := BuiltinList()
	seen := make(map[string]struct{}, len(out))
	for _, t := range out {
		seen[t.Name] = struct{}{}
	}
	users, err := s.ListUser()
	if err != nil {
		return nil, err
	}
	for _, t := range users {
		if _, dup := seen[t.Name]; dup {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GetAny returns the template by name, preferring builtin over user.
func (s *Store) GetAny(name string) (Template, bool, error) {
	if t, ok := BuiltinGet(name); ok {
		return t, true, nil
	}
	return s.GetUser(name)
}

// Upsert stores a user template. If create is true and the row exists it returns an error.
func (s *Store) Upsert(t Template, create bool) error {
	if s == nil || s.DB == nil {
		return errors.New("template store not configured")
	}
	if t.Name == "" {
		return errors.New("template name is required")
	}
	if _, ok := BuiltinGet(t.Name); ok {
		return ErrConflictBuiltin
	}
	if err := t.Pipeline.Validate(); err != nil {
		return fmt.Errorf("pipeline invalid: %w", err)
	}
	t.Source = "user"
	spec, err := sonic.MarshalString(t)
	if err != nil {
		return err
	}
	var row models.CollectorTemplate
	err = s.DB.Where("name = ?", t.Name).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = models.CollectorTemplate{
			Name:        t.Name,
			Description: t.Description,
			Spec:        spec,
		}
		return s.DB.Create(&row).Error
	}
	if err != nil {
		return err
	}
	if create {
		return fmt.Errorf("template %q already exists", t.Name)
	}
	row.Description = t.Description
	row.Spec = spec
	return s.DB.Save(&row).Error
}

func (s *Store) Delete(name string) error {
	if s == nil || s.DB == nil {
		return errors.New("template store not configured")
	}
	if _, ok := BuiltinGet(name); ok {
		return ErrConflictBuiltin
	}
	res := s.DB.Where("name = ?", name).Delete(&models.CollectorTemplate{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func decode(spec string) (Template, error) {
	var t Template
	if spec == "" {
		return t, nil
	}
	if err := sonic.UnmarshalString(spec, &t); err != nil {
		return t, err
	}
	return t, nil
}
