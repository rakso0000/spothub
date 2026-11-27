package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/rakso0000/spothub/backend/internal/auth"
)

type ratingRequest struct {
	Surface  int `json:"surface"`
	Lighting int `json:"lighting"`
	Security int `json:"security"`
}

type SpotDetail struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	Sports      []string `json:"sports"`
	Author      *string  `json:"author"`
	Surface     *float64 `json:"surface"`
	Lighting    *float64 `json:"lighting"`
	Security    *float64 `json:"security"`
	RatingCount int      `json:"rating_count"`
}

func spotID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id > 0
}

func validScore(v int) bool { return v >= 1 && v <= 5 }

// PUT /api/spots/{id}/rating (нужен токен)
func (h *SpotsHandler) Rate(w http.ResponseWriter, r *http.Request) {
	id, ok := spotID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id спота")
		return
	}

	var req ratingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	if !validScore(req.Surface) || !validScore(req.Lighting) || !validScore(req.Security) {
		writeError(w, http.StatusBadRequest, "оценки: целые числа от 1 до 5")
		return
	}

	var exists bool
	err := h.DB.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM spots WHERE id=$1 AND status='approved')`, id).Scan(&exists)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "спот не найден")
		return
	}

	_, err = h.DB.Exec(r.Context(), `
		INSERT INTO spot_ratings (spot_id, user_id, surface, lighting, security)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (spot_id, user_id) DO UPDATE
		SET surface = EXCLUDED.surface,
		    lighting = EXCLUDED.lighting,
		    security = EXCLUDED.security,
		    updated_at = now()`,
		id, auth.UserID(r.Context()), req.Surface, req.Lighting, req.Security)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось сохранить оценку")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /api/spots/{id}
func (h *SpotsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := spotID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id спота")
		return
	}

	var d SpotDetail
	err := h.DB.QueryRow(r.Context(), `
		SELECT s.id, s.title, s.description,
		       ST_Y(s.location::geometry), ST_X(s.location::geometry), s.sports,
		       u.nickname,
		       AVG(r.surface)::float8, AVG(r.lighting)::float8, AVG(r.security)::float8,
		       COUNT(r.user_id)
		FROM spots s
		LEFT JOIN users u ON u.id = s.author_id
		LEFT JOIN spot_ratings r ON r.spot_id = s.id
		WHERE s.id = $1 AND s.status = 'approved'
		GROUP BY s.id, u.nickname`, id).
		Scan(&d.ID, &d.Title, &d.Description, &d.Lat, &d.Lon, &d.Sports,
			&d.Author, &d.Surface, &d.Lighting, &d.Security, &d.RatingCount)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "спот не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	writeJSON(w, http.StatusOK, d)
}
