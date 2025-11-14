package config

import (
    "fmt"
    "os"
    "strconv"
)

type Config struct {
    HTTPPort      string
    PostgresDSN   string
    RedisAddr     string
    RedisPassword string
    RedisDB       int
    ResultsToken  string
    AgentsCount   int
    TaskTTLSeconds int
    AdminToken    string
    AdminUser     string
    AdminPass     string
    PublicAPIBase string
    AgentImage    string
    DockerNetwork string
    ExternalRedisPort string
}

func getEnv(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}

func Load() Config {
    cfg := Config{
        HTTPPort:      getEnv("API_PORT", "8080"),
        PostgresDSN:   getEnv("POSTGRES_DSN", "postgres://postgres:postgres@postgres:5432/aeza?sslmode=disable"),
        RedisAddr:     getEnv("REDIS_ADDR", "redis:6379"),
        RedisPassword: getEnv("REDIS_PASSWORD", ""),
        ResultsToken:  getEnv("RESULTS_TOKEN", "dev-token"),
        AgentsCount:   3,
        TaskTTLSeconds: 90,
        AdminToken:    getEnv("ADMIN_TOKEN", "admin-dev"),
        AdminUser:     getEnv("ADMIN_USER", "admin"),
        AdminPass:     getEnv("ADMIN_PASS", "admin"),
        PublicAPIBase: getEnv("PUBLIC_API_BASE", "http://api:8080"),
        AgentImage:    getEnv("AGENT_IMAGE", "aeza-agent:latest"),
        DockerNetwork: getEnv("DOCKER_NETWORK", "aeza_default"),
        ExternalRedisPort: getEnv("EXTERNAL_REDIS_PORT", "6379"),
    }
    if v := os.Getenv("REDIS_DB"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            cfg.RedisDB = n
        } else {
            _, _ = fmt.Fprintf(os.Stderr, "invalid REDIS_DB %q: %v\n", v, err)
        }
    }
    if v := os.Getenv("AGENTS_COUNT"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 {
            cfg.AgentsCount = n
        }
    }
    if v := os.Getenv("TASK_TTL_SECONDS"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 {
            cfg.TaskTTLSeconds = n
        }
    }
    return cfg
}


