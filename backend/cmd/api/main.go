package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/rakso0000/spothub/backend/internal/auth"
	"github.com/rakso0000/spothub/backend/internal/config"
	"github.com/rakso0000/spothub/backend/internal/handlers"
	"github.com/rakso0000/spothub/backend/internal/security"
	"github.com/rakso0000/spothub/backend/internal/ws"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

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
		AllowedOrigins: cfg.CORSOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
	}))
	r.Use(security.Headers)
	r.Use(security.MaxBody(1 << 20)) // 1 МБ
	general := security.NewLimiter(rate.Limit(20), 40)
	r.Use(general.Middleware)

	// Жёсткий лимит на регистрацию и логин: 5 подряд, потом 1 в 12 секунд.
	authLimiter := security.NewLimiter(rate.Every(12*time.Second), 5)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	secret := []byte(cfg.JWTSecret)
	hub := ws.NewHub()

	authH := &handlers.AuthHandler{DB: pool, Secret: secret}
	r.With(authLimiter.Middleware).Post("/api/auth/register", authH.Register)
	r.With(authLimiter.Middleware).Post("/api/auth/login", authH.Login)

	spotsH := &handlers.SpotsHandler{DB: pool}
	r.Get("/api/spots", spotsH.List)
	r.Get("/api/spots/{id}", spotsH.Get)

	chatH := &handlers.ChatHandler{DB: pool, Hub: hub, Secret: secret}
	r.Get("/api/spots/{id}/messages", chatH.History)
	r.Get("/ws/spots/{id}", chatH.Serve) // токен проверяется внутри, из ?token=

	checkinH := &handlers.CheckinHandler{DB: pool, Hub: hub}
	r.Get("/api/spots/{id}/here", checkinH.Here)
	go checkinH.RunCleanup(context.Background(), 30*time.Second)

	modH := &handlers.ModerationHandler{DB: pool, Hub: hub}

	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(secret))
		r.Use(handlers.ActiveUser(pool)) // забаненные дальше не проходят

		r.With(handlers.LimitSpotCreation(pool, 10)).Post("/api/spots", spotsH.Create)
		r.Put("/api/spots/{id}/rating", spotsH.Rate)
		r.Post("/api/spots/{id}/checkin", checkinH.Checkin)
		r.Delete("/api/spots/{id}/checkin", checkinH.Checkout)
		r.Post("/api/reports", modH.Report)

		r.Route("/api/admin", func(r chi.Router) {
			r.Use(handlers.RequireModerator(pool))
			r.Get("/reports", modH.ListReports)
			r.Post("/reports/{id}/resolve", modH.ResolveReport)
			r.Get("/spots/pending", modH.PendingSpots)
			r.Post("/spots/{id}/approve", modH.ApproveSpot)
			r.Post("/spots/{id}/hide", modH.HideSpot)
			r.Post("/messages/{id}/hide", modH.HideMessage)
			r.Post("/users/{id}/ban", modH.Ban)
			r.Post("/users/{id}/unban", modH.Unban)
		})
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Println("listening on :" + cfg.Port)
	log.Fatal(srv.ListenAndServe())
}
