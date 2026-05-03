package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/config"

	"github.com/spf13/viper"
)

// Viper holds singleton state, so each test resets it explicitly. Without
// this, SetDefault calls would accumulate across cases and previously-set
// values would leak into the next LoadConfig.
func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
}

// chdirToTemp writes the supplied .env contents to a tempdir and cd's into
// it. LoadConfig reads `./` for `.env`, so this is the cleanest way to drive
// the loader the same way production does — through a real .env file. An
// empty body writes no file, exercising the missing-.env path.
func chdirToTemp(t *testing.T, envBody string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if envBody != "" {
		if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte(envBody), 0o600); err != nil {
			t.Fatalf("write .env: %v", err)
		}
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestLoadConfig_FullDotEnv(t *testing.T) {
	resetViper(t)
	chdirToTemp(t, `
ServerPort=9090
PprofPort=7070
LogLevel=debug
DatabaseURL=postgres://test
RedisURL=redis://test:6379/3
RedisOpTimeout=250ms
RateLimitPerMinute=42
RateLimitWindow=30s
ServiceName=svc-test
ServiceVersion=1.2.3
OtelExporterEndpoint=http://collector:4318
OtelSampleRatio=0.25
CleanUpInterval=5m
URLUnusedThreshold=12h
`)

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"ServerPort", cfg.ServerPort, 9090},
		{"PprofPort", cfg.PprofPort, 7070},
		{"LogLevel", cfg.LogLevel, "debug"},
		{"DatabaseURL", cfg.DatabaseURL, "postgres://test"},
		{"RedisURL", cfg.RedisURL, "redis://test:6379/3"},
		{"RedisOpTimeout", cfg.RedisOpTimeout, 250 * time.Millisecond},
		{"RateLimitPerMinute", cfg.RateLimitPerMinute, 42},
		{"RateLimitWindow", cfg.RateLimitWindow, 30 * time.Second},
		{"ServiceName", cfg.ServiceName, "svc-test"},
		{"ServiceVersion", cfg.ServiceVersion, "1.2.3"},
		{"OtelExporterEndpoint", cfg.OtelExporterEndpoint, "http://collector:4318"},
		{"OtelSampleRatio", cfg.OtelSampleRatio, 0.25},
		{"CleanUpInterval", cfg.CleanUpInterval, 5 * time.Minute},
		{"URLUnusedThreshold", cfg.URLUnusedThreshold, 12 * time.Hour},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoadConfig_MissingEnvFile_DoesNotError(t *testing.T) {
	resetViper(t)
	chdirToTemp(t, "") // no .env written

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig should tolerate a missing .env, got %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg is nil")
	}
}

func TestLoadConfig_PartialDotEnv_LeavesOtherFieldsZero(t *testing.T) {
	resetViper(t)
	chdirToTemp(t, "ServerPort=4040\n")

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ServerPort != 4040 {
		t.Errorf("ServerPort: got %d want 4040", cfg.ServerPort)
	}
}
