package config

import "testing"

func TestDefaultNetworkSettings(t *testing.T) {
	cfg := Default()
	if cfg.Network.MaxConcurrentDownloads != 5 {
		t.Errorf("max concurrent downloads = %d, want 5", cfg.Network.MaxConcurrentDownloads)
	}
	if cfg.Network.RetryCount != 3 {
		t.Errorf("retry count = %d, want 3", cfg.Network.RetryCount)
	}
}
