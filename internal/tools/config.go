package tools

import (
	"fmt"
	"os"
)

type Config struct {
	RedisURL    string
	DatabaseURL string
	WorkerCount int
	Port        string
}

func LoadConfig() *Config {
	return &Config{
		RedisURL:    getEnv("REDIS_URL", "redis:6379"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://user:password@postgres:5432/payment_system?sslmode=disable"),
		WorkerCount: getEnvInt("WORKER_COUNT", 10),
		Port:        getEnv("PORT", "8080"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var result int
		if _, err := fmt.Sscanf(value, "%d", &result); err == nil {
			return result
		}
	}
	return defaultValue
}
