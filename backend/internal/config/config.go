package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	Env         string
	Port        string
	DBURL       string
	JWTSecret   string
	CORSOrigins []string
}

func Load() Config {
	origins := get("CORS_ORIGINS", "http://localhost:*,http://127.0.0.1:*")
	list := []string{}
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			list = append(list, o)
		}
	}
	return Config{
		Env:         get("APP_ENV", "dev"),
		Port:        get("PORT", "8080"),
		DBURL:       get("DB_URL", "postgres://spothub:spothub@localhost:5432/spothub?sslmode=disable"),
		JWTSecret:   get("JWT_SECRET", "dev-secret-change-me"),
		CORSOrigins: list,
	}
}

// Validate не даёт запустить production со слабым секретом.
func (c Config) Validate() error {
	if c.Env == "production" && (c.JWTSecret == "dev-secret-change-me" || len(c.JWTSecret) < 32) {
		return errors.New("в production нужен JWT_SECRET длиной от 32 символов")
	}
	return nil
}

func get(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
