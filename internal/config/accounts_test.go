package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccountPolicyDefaultsAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"accounts":{"pollIntervalMs":2000},"cdpPort":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.Get()
	if cfg.Accounts.PollIntervalMS != 2000 || cfg.Accounts.MaxResponseBytes != DefaultAccountConfig().MaxResponseBytes || cfg.CdpPort != 0 {
		t.Fatal("partial config lost defaults/automatic port")
	}
	for _, bad := range []func(*AccountConfig){
		func(c *AccountConfig) { c.PollIntervalMS = 0 },
		func(c *AccountConfig) { c.PollMaxIntervalMS = c.PollIntervalMS - 1 },
		func(c *AccountConfig) { c.RequestTimeoutSeconds = 0 },
		func(c *AccountConfig) { c.LoginTimeoutSeconds = 1 },
		func(c *AccountConfig) { c.RefreshCooldownSeconds = 0 },
		func(c *AccountConfig) { c.MaxResponseBytes = 0 },
	} {
		policy := DefaultAccountConfig()
		bad(&policy)
		if err := policy.Validate(); err == nil {
			t.Fatalf("invalid policy accepted: %+v", policy)
		}
	}
}
