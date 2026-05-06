package cloud

import (
	"os"
	"strconv"
	"strings"

	"github.com/Gentleman-Programming/engram/internal/product"
)

type Config struct {
	DSN             string
	JWTSecret       string
	CORSOrigins     []string
	MaxPool         int
	Port            int
	BindHost        string
	AdminToken      string
	AllowedProjects []string
	// IntuitHub integration. When IntuitHubBaseURL is set, the cloud server
	// uses IntuitHub for organizational identity. AdminKey is required for
	// the bulk sync job; the auth middleware validates user keys via /me.
	IntuitHubBaseURL  string
	IntuitHubAdminKey string
}

const DefaultJWTSecret = "engram-dev-jwt-secret-for-local-smoke-1234"

func DefaultConfig() Config {
	return Config{
		DSN:         "postgres://engram:engram_dev@localhost:5433/engram_cloud?sslmode=disable",
		JWTSecret:   DefaultJWTSecret,
		CORSOrigins: []string{"*"},
		MaxPool:     10,
		Port:        8080,
		BindHost:    "127.0.0.1",
	}
}

func IsDefaultJWTSecret(secret string) bool {
	return strings.TrimSpace(secret) == DefaultJWTSecret
}

func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	if v := strings.TrimSpace(os.Getenv(product.EnvDatabaseURL)); v != "" {
		cfg.DSN = v
	} else if v := strings.TrimSpace(os.Getenv(product.LegacyEnvDatabaseURL)); v != "" {
		cfg.DSN = v
	}
	if v := strings.TrimSpace(os.Getenv(product.EnvJWTSecret)); v != "" {
		cfg.JWTSecret = v
	} else if v := strings.TrimSpace(os.Getenv(product.LegacyEnvJWTSecret)); v != "" {
		cfg.JWTSecret = v
	}
	if v := strings.TrimSpace(os.Getenv(product.EnvCloudAdmin)); v != "" {
		cfg.AdminToken = v
	} else if v := strings.TrimSpace(os.Getenv(product.LegacyEnvCloudAdmin)); v != "" {
		cfg.AdminToken = v
	}
	if v := strings.TrimSpace(os.Getenv("ENGRAM_PORT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Port = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("ENGRAM_CLOUD_HOST")); v != "" {
		cfg.BindHost = v
	}
	if v := strings.TrimSpace(os.Getenv(product.EnvIntuitHubBaseURL)); v != "" {
		cfg.IntuitHubBaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv(product.EnvIntuitHubAdminKey)); v != "" {
		cfg.IntuitHubAdminKey = v
	}
	if v := strings.TrimSpace(os.Getenv("ENGRAM_CLOUD_ALLOWED_PROJECTS")); v != "" {
		parts := strings.Split(v, ",")
		projects := make([]string, 0, len(parts))
		seen := make(map[string]struct{})
		for _, part := range parts {
			project := strings.TrimSpace(part)
			if project == "" {
				continue
			}
			if _, ok := seen[project]; ok {
				continue
			}
			seen[project] = struct{}{}
			projects = append(projects, project)
		}
		cfg.AllowedProjects = projects
	}
	return cfg
}
