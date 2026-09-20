package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryRetentionConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		env  string
		want int
	}{
		{name: "default", want: 7},
		{name: "yaml", yaml: "history:\n  retention_days: 30\n", want: 30},
		{name: "environment", yaml: "history:\n  retention_days: 30\n", env: "14", want: 14},
		{name: "zero", yaml: "history:\n  retention_days: 0\n"},
		{name: "negative", env: "-1"},
		{name: "invalid", env: "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Isolate the optional environment override from the developer's config.
			t.Setenv("SIPHON_HISTORY__RETENTION_DAYS", tc.env)
			if tc.env == "" {
				if err := os.Unsetenv("SIPHON_HISTORY__RETENTION_DAYS"); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if tc.want == 0 {
				if err == nil {
					t.Fatal("expected invalid retention to be rejected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.History.RetentionDays != tc.want {
				t.Fatalf("retention = %d, want %d", cfg.History.RetentionDays, tc.want)
			}
		})
	}
}
