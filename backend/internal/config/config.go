package config

import "os"

type Config struct {
	Port      string
	DBURL     string
	JWTSecret string
}

func Load() Config {
	return Config{
		Port:      get("PORT", "8080"),
		DBURL:     get("DB_URL", "postgres://spothub:spothub@localhost:5432/spothub?sslmode=disable"),
		JWTSecret: get("JWT_SECRET", "dev-secret-change-me"),
	}
}

func get(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
