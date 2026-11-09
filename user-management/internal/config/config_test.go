package config

import (
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := load(env(map[string]string{"JWT_SECRET": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.GRPCAddr != ":9090" || cfg.UserCountInterval != 10*time.Second || cfg.JWTTTL != time.Hour || !cfg.SwaggerEnabled {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := load(env(map[string]string{
		"JWT_SECRET": "x", "HTTP_ADDR": ":1", "JWT_TTL": "15m", "MONGO_URI": "mongodb://db:27017", "SWAGGER_ENABLED": "false",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":1" || cfg.JWTTTL != 15*time.Minute || cfg.MongoURI != "mongodb://db:27017" || cfg.SwaggerEnabled {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoad_Errors(t *testing.T) {
	if _, err := load(env(map[string]string{})); err == nil {
		t.Error("missing JWT_SECRET accepted")
	}
	if _, err := load(env(map[string]string{"JWT_SECRET": "x", "JWT_TTL": "forever"})); err == nil {
		t.Error("invalid duration accepted")
	}
	if _, err := load(env(map[string]string{"JWT_SECRET": "x", "USER_COUNT_INTERVAL": "-1s"})); err == nil {
		t.Error("negative duration accepted")
	}
	if _, err := load(env(map[string]string{"JWT_SECRET": "x", "SWAGGER_ENABLED": "maybe"})); err == nil {
		t.Error("invalid boolean accepted")
	}
}
