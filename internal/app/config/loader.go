// loader.go loads etcdisc configuration from YAML with environment variable overrides.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const envPrefix = "ETCDISC_"

// Load returns the runtime configuration using defaults, optional YAML, and env overrides.
func Load() (Config, error) {
	cfg := Default()
	if err := loadYAML(&cfg); err != nil {
		return Config{}, err
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadYAML(cfg *Config) error {
	path := os.Getenv(envPrefix + "CONFIG_FILE")
	if path == "" {
		path = filepath.Join("deployments", "etcdisc.yaml")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return yaml.Unmarshal(payload, cfg)
}

func applyEnv(cfg *Config) error {
	applyString(&cfg.App.Name, envPrefix+"APP_NAME")
	applyString(&cfg.App.Env, envPrefix+"APP_ENV")
	applyString(&cfg.HTTP.Host, envPrefix+"HTTP_HOST")
	if err := applyInt(&cfg.HTTP.Port, envPrefix+"HTTP_PORT"); err != nil {
		return err
	}
	applyString(&cfg.GRPC.Host, envPrefix+"GRPC_HOST")
	if err := applyInt(&cfg.GRPC.Port, envPrefix+"GRPC_PORT"); err != nil {
		return err
	}
	if endpoints := os.Getenv(envPrefix + "ETCD_ENDPOINTS"); endpoints != "" {
		cfg.Etcd.Endpoints = splitAndTrim(endpoints)
	}
	if err := applyInt(&cfg.Etcd.DialMS, envPrefix+"ETCD_DIAL_MS"); err != nil {
		return err
	}
	applyString(&cfg.Admin.Token, envPrefix+"ADMIN_TOKEN")
	return nil
}

func applyString(target *string, envKey string) {
	if value := os.Getenv(envKey); value != "" {
		*target = value
	}
}

func applyInt(target *int, envKey string) error {
	value := os.Getenv(envKey)
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	*target = parsed
	return nil
}

func splitAndTrim(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
