// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	HTTPAddr          string
	GRPCAddr          string
	MongoURI          string
	MongoDatabase     string
	JWTSecret         string
	JWTIssuer         string
	JWTTTL            time.Duration
	UserCountInterval time.Duration
	ShutdownTimeout   time.Duration
	LogLevel          string
	SwaggerEnabled    bool
}

// Load reads configuration from the environment, applying defaults.
// JWT_SECRET is mandatory: there is deliberately no insecure default.
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	var errs []error
	str := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	dur := func(key string, def time.Duration) time.Duration {
		v := getenv(key)
		if v == "" {
			return def
		}
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s: must be a positive duration like 10s or 1h, got %q", key, v))
			return def
		}
		return d
	}
	boolean := func(key string, def bool) bool {
		v := getenv(key)
		if v == "" {
			return def
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: must be true or false, got %q", key, v))
			return def
		}
		return b
	}

	cfg := Config{
		HTTPAddr:          str("HTTP_ADDR", ":8080"),
		GRPCAddr:          str("GRPC_ADDR", ":9090"),
		MongoURI:          str("MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase:     str("MONGO_DATABASE", "user_management"),
		JWTSecret:         getenv("JWT_SECRET"),
		JWTIssuer:         str("JWT_ISSUER", "user-management-api"),
		JWTTTL:            dur("JWT_TTL", time.Hour),
		UserCountInterval: dur("USER_COUNT_INTERVAL", 10*time.Second),
		ShutdownTimeout:   dur("SHUTDOWN_TIMEOUT", 15*time.Second),
		LogLevel:          str("LOG_LEVEL", "info"),
		SwaggerEnabled:    boolean("SWAGGER_ENABLED", true),
	}
	if cfg.JWTSecret == "" {
		errs = append(errs, errors.New("JWT_SECRET is required (at least 32 bytes)"))
	}
	return cfg, errors.Join(errs...)
}
