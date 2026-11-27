package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakso0000/spothub/backend/internal/auth"
)

type SpotsHandler struct {
	DB *pgxpool.Pool
}

type Spot struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	Sports      []string `json:"sports"`
}

type createSpotRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	Sports      []string `json:"sports"`
}

var allowedSports = map[string]bool{
	"skate": true, "scooter": true, "roller": true, "bmx": true,
}

// POST /api/spots (нужен токен)
func (h *SpotsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createSpotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)

	if n := len([]rune(req.Title)); n < 3 || n > 100 {
		writeError(w, http.StatusBadRequest, "название: от 3 до 100 символов")
		return
	}
	if len([]rune(req.Description)) > 1000 {
		writeError(w, http.StatusBadRequest, "описание: до 1000 символов")
		return
	}
	if req.Lat < -90 || req.Lat > 90 || req.Lon < -180 || req.Lon > 180 {
		writeError(w, http.StatusBadRequest, "неверные координаты")
		return
	}
	if len(req.Sports) == 0 {
		writeError(w, http.StatusBadRequest, "укажите хотя бы один вид спорта")
		return
	}
	for _, s := range req.Sports {
		if !allowedSports[s] {
			writeError(w, http.StatusBadRequest, "неизвестный вид спорта: "+s)
			return
		}
	}

	var id int64
	err := h.DB.QueryRow(r.Context(), `
		INSERT INTO spots (author_id, title, description, location, sports)
		VALUES ($1, $2, $3, ST_SetSRID(ST_MakePoint($4,$5),4326)::geography, $6)
		RETURNING id`,
		auth.UserID(r.Context()), req.Title, req.Description, req.Lon, req.Lat, req.Sports).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось создать спот")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// GET /api/spots?bbox=lon1,lat1,lon2,lat2&sport=skate
func (h *SpotsHandler) List(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Query().Get("bbox"), ",")
	if len(parts) != 4 {
		writeError(w, http.StatusBadRequest, "bbox: lon1,lat1,lon2,lat2")
		return
	}
	var b [4]float64
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "неверный bbox")
			return
		}
		b[i] = v
	}
	if b[0] >= b[2] || b[1] >= b[3] {
		writeError(w, http.StatusBadRequest, "bbox: сначала юго-запад, потом северо-восток")
		return
	}

	sport := r.URL.Query().Get("sport")
	if sport != "" && !allowedSports[sport] {
		writeError(w, http.StatusBadRequest, "неизвестный вид спорта")
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT id, title, description,
		       ST_Y(location::geometry), ST_X(location::geometry), sports
		FROM spots
		WHERE status = 'approved'
		  AND ST_Intersects(location, ST_MakeEnvelope($1,$2,$3,$4,4326)::geography)
		  AND ($5 = '' OR $5 = ANY(sports))
		LIMIT 500`, b[0], b[1], b[2], b[3], sport)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	spots := []Spot{}
	for rows.Next() {
		var s Spot
		if err := rows.Scan(&s.ID, &s.Title, &s.Description, &s.Lat, &s.Lon, &s.Sports); err != nil {
			writeError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		spots = append(spots, s)
	}
	writeJSON(w, http.StatusOK, spots)
}
