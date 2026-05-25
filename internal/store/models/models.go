package models

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Username     string `gorm:"uniqueIndex;size:64" json:"username"`
	PasswordHash string `gorm:"size:255" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Site struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Name      string         `gorm:"size:128;index" json:"name"`
	BaseURL   string         `gorm:"size:512" json:"base_url"`
	Tags      string         `gorm:"size:255" json:"tags"`
	Notes     string         `gorm:"type:text" json:"notes"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

type Credential struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	SiteID    uint           `gorm:"index" json:"site_id"`
	Name      string         `gorm:"size:128" json:"name"`
	Type      string         `gorm:"size:32" json:"type"`
	Payload   string         `gorm:"type:text" json:"-"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

type Collector struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	SiteID       uint           `gorm:"index" json:"site_id"`
	Name         string         `gorm:"size:128;index" json:"name"`
	Description  string         `gorm:"type:text" json:"description"`
	PipelineJSON string         `gorm:"type:text" json:"pipeline_json"`
	ScheduleType string         `gorm:"size:16" json:"schedule_type"` // none|interval|cron|event
	ScheduleSpec string         `gorm:"size:128" json:"schedule_spec"`
	Enabled      bool           `gorm:"index" json:"enabled"`
	Timeout      int            `json:"timeout"`
	LastRunAt    *time.Time     `json:"last_run_at"`
	LastStatus   string         `gorm:"size:16" json:"last_status"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

type Indicator struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	CollectorID uint           `gorm:"index" json:"collector_id"`
	Key         string         `gorm:"size:64" json:"key"`
	Name        string         `gorm:"size:128" json:"name"`
	Type        string         `gorm:"size:16" json:"type"` // number|string|bool|json
	Unit        string         `gorm:"size:32" json:"unit"`
	Display     string         `gorm:"size:16" json:"display"` // gauge|line|table
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

type Run struct {
	ID          uint64     `gorm:"primaryKey" json:"id"`
	CollectorID uint       `gorm:"index" json:"collector_id"`
	Trigger     string     `gorm:"size:16" json:"trigger"`
	Status      string     `gorm:"size:16;index" json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	DurationMS  int64      `json:"duration_ms"`
	Error       string     `gorm:"type:text" json:"error"`
}

type StepLog struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	RunID      uint64    `gorm:"index" json:"run_id"`
	Index      int       `json:"index"`
	Kind       string    `gorm:"size:64" json:"kind"`
	Snippet    string    `gorm:"type:text" json:"snippet"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `gorm:"type:text" json:"error"`
	CreatedAt  time.Time `json:"created_at"`
}

type DataPoint struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`
	CollectorID uint      `gorm:"index:idx_dp_collector_indicator_ts,priority:1" json:"collector_id"`
	IndicatorID uint      `gorm:"index:idx_dp_collector_indicator_ts,priority:2" json:"indicator_id"`
	RunID       uint64    `gorm:"index" json:"run_id"`
	ValueNum    *float64  `json:"value_num"`
	ValueStr    *string   `json:"value_str"`
	ValueJSON   *string   `gorm:"type:text" json:"value_json"`
	Ts          time.Time `gorm:"index:idx_dp_collector_indicator_ts,priority:3" json:"ts"`
}

type CollectorTemplate struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Name        string         `gorm:"uniqueIndex;size:128" json:"name"`
	Description string         `gorm:"type:text" json:"description"`
	Spec        string         `gorm:"type:text" json:"-"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}
