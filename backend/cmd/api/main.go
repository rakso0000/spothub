package main

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakso0000/spothub/backend/internal/auth"
	"github.com/rakso0000/spothub/backend/internal/config"
	"github.com/rakso0000/spothub/backend/internal/handlers"
	"github.com/rakso0000/spothub/backend/internal/ws"
)

func main() {
	cfg := config.Load()

	pool, err := pgxpool.New(context.Background(), cfg.DBURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatal("база недоступна: ", err)
	}

	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	secret := []byte(cfg.JWTSecret)

	authH := &handlers.AuthHandler{DB: pool, Secret: secret}
	r.Post("/api/auth/register", authH.Register)
	r.Post("/api/auth/login", authH.Login)

	spotsH := &handlers.SpotsHandler{DB: pool}
	r.Get("/api/spots", spotsH.List)
	r.Get("/api/spots/{id}", spotsH.Get)

	hub := ws.NewHub()
	chatH := &handlers.ChatHandler{DB: pool, Hub: hub, Secret: secret}
	r.Get("/api/spots/{id}/messages", chatH.History)
	r.Get("/ws/spots/{id}", chatH.Serve) // токен проверяется внутри, из ?token=

	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(secret))
		r.Post("/api/spots", spotsH.Create)
		r.Put("/api/spots/{id}/rating", spotsH.Rate)
	})

	log.Println("listening on :" + cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, r))
}
