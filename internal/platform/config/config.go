package config

import (
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	ServerPort         int
	PprofPort          int
	DatabaseURL        string
	ReadDatabaseURL    string
	CleanUpInterval    time.Duration
	URLUnusedThreshold time.Duration
	LogLevel           string

	// Redis — backs the distributed rate limiter so multiple replicas share
	// a single quota.
	RedisURL       string
	RedisOpTimeout time.Duration

	// Rate limiter — fixed window per client IP.
	RateLimitPerMinute int
	RateLimitWindow    time.Duration

	// Observability — OpenTelemetry. Empty endpoint = noop tracer (no
	// network calls, no startup failure when no collector is around).
	ServiceName          string
	ServiceVersion       string
	OtelExporterEndpoint string
	OtelExporterInsecure bool
	OtelSampleRatio      float64
}

func LoadConfig() (*Config, error) {
	// 1. Tell Viper where to look for the file
	viper.AddConfigPath(".")
	viper.SetConfigName(".env")
	viper.SetConfigType("env")

	// 2. Allow environment variables to override file settings
	viper.AutomaticEnv()

	// Set defaults
	viper.SetDefault("SERVER_PORT", "8080")
	viper.SetDefault("PPROF_PORT", "6060")
	viper.SetDefault("CLEANUP_INTERVAL", "1m")
	viper.SetDefault("URL_UNUSED_THRESHOLD", "24h")
	viper.SetDefault("LOG_LEVEL", "info")
	viper.SetDefault("REDIS_URL", "redis://localhost:6379/0")
	viper.SetDefault("REDIS_OP_TIMEOUT", "100ms")
	viper.SetDefault("RATE_LIMIT_PER_MINUTE", "100")
	viper.SetDefault("RATE_LIMIT_WINDOW", "1m")
	viper.SetDefault("SERVICE_NAME", "url-shortener")
	viper.SetDefault("SERVICE_VERSION", "dev")
	viper.SetDefault("OTEL_EXPORTER_ENDPOINT", "")
	viper.SetDefault("OTEL_EXPORTER_INSECURE", "false")
	viper.SetDefault("OTEL_SAMPLE_RATIO", "1.0")

	if err := viper.ReadInConfig(); err != nil {
		// It's okay if the .env file is missing, we might be using Env Vars
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
