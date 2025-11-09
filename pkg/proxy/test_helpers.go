package proxy

import (
	"fmt"
	"time"

	"github.com/verastack/telephone/pkg/config"
	"github.com/verastack/telephone/pkg/storage"
)

// initTestTokenStore creates a test token store for unit tests
func initTestTokenStore(cfg *config.Config) (*storage.TokenStore, error) {
	if cfg.TokenDBPath == "" || cfg.SecretKeyBase == "" {
		return nil, fmt.Errorf("TokenDBPath and SecretKeyBase required")
	}

	timeout := cfg.DBTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	return storage.NewTokenStore(cfg.TokenDBPath, cfg.SecretKeyBase, timeout)
}
