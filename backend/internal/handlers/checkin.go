package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakso0000/spothub/backend/internal/auth"
	"github.com/rakso0000/spothub/backend/internal/ws"
)

const checkinTTL = 2 * time.Hour

type CheckinHandler struct {
	DB  *pgxpool.Pool
	Hub *ws.Hub
}

type presenceEvent struct {
	Type      string     `json:"type"` // "checkin" или "checkout"
	SpotID    int64      `json:"spot_id"`
	UserID    int64      `json:"user_id"`
	User      string     `json:"user"`
	TS        time.Time  `json:"ts"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (h *CheckinHandler) broadcast(typ string, spotID, userID int64, nick string, expires *time.Time) {
	data, err := json.Marshal(presenceEvent{
		Type: typ, SpotID: spotID, UserID: userID, User: nick,
		TS: time.Now().UTC(), ExpiresAt: expires,
	})
	if err != nil {
		return
	}
	h.Hub.Broadcast(spotID, data)
}

// POST /api/spots/{id}/checkin (нужен токен)
func (h *CheckinHandler) Checkin(w http.ResponseWriter, r *http.Request) {
	sid, ok := spotID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id спота")
		return
	}
	uid := auth.UserID(r.Context())

	var nick string
	err := h.DB.QueryRow(r.Context(), `
		SELECT u.nickname
		FROM users u
		JOIN spots s ON s.id = $2 AND s.status = 'approved'
		WHERE u.id = $1`, uid, sid).Scan(&nick)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "спот не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}

	// Человек может быть только на одном споте: уходим с остальных.
	rows, err := h.DB.Query(r.Context(),
		`DELETE FROM checkins WHERE user_id = $1 AND spot_id <> $2 RETURNING spot_id`, uid, sid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	var left []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			left = append(left, id)
		}
	}
	rows.Close()
	for _, id := range left {
		h.broadcast("checkout", id, uid, nick, nil)
	}

	expires := time.Now().Add(checkinTTL).UTC()
	_, err = h.DB.Exec(r.Context(), `
		INSERT INTO checkins (spot_id, user_id, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (spot_id, user_id) DO UPDATE SET expires_at = EXCLUDED.expires_at`,
		sid, uid, expires)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось отметиться")
		return
	}

	h.broadcast("checkin", sid, uid, nick, &expires)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "expires_at": expires})
}

// DELETE /api/spots/{id}/checkin (нужен токен)
func (h *CheckinHandler) Checkout(w http.ResponseWriter, r *http.Request) {
	sid, ok := spotID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id спота")
		return
	}
	uid := auth.UserID(r.Context())

	var nick string
	err := h.DB.QueryRow(r.Context(), `
		WITH d AS (
			DELETE FROM checkins WHERE spot_id = $1 AND user_id = $2 RETURNING user_id
		)
		SELECT u.nickname FROM d JOIN users u ON u.id = d.user_id`, sid, uid).Scan(&nick)
	if err == nil {
		h.broadcast("checkout", sid, uid, nick, nil)
	} else if err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	// Если отметки не было, тоже отвечаем ok: операция идемпотентна.
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type hereUser struct {
	UserID    int64     `json:"user_id"`
	User      string    `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
}

// GET /api/spots/{id}/here
func (h *CheckinHandler) Here(w http.ResponseWriter, r *http.Request) {
	sid, ok := spotID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id спота")
		return
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT u.id, u.nickname, c.expires_at
		FROM checkins c
		JOIN users u ON u.id = c.user_id
		WHERE c.spot_id = $1 AND c.expires_at > $2
		ORDER BY c.expires_at DESC`, sid, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	list := []hereUser{}
	for rows.Next() {
		var u hereUser
		if err := rows.Scan(&u.UserID, &u.User, &u.ExpiresAt); err != nil {
			writeError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		list = append(list, u)
	}
	writeJSON(w, http.StatusOK, list)
}

// RunCleanup периодически удаляет просроченные отметки и рассылает checkout.
func (h *CheckinHandler) RunCleanup(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.cleanup(ctx)
		}
	}
}

func (h *CheckinHandler) cleanup(ctx context.Context) {
	rows, err := h.DB.Query(ctx, `
		WITH d AS (
			DELETE FROM checkins WHERE expires_at <= $1 RETURNING spot_id, user_id
		)
		SELECT d.spot_id, d.user_id, u.nickname FROM d JOIN users u ON u.id = d.user_id`,
		time.Now())
	if err != nil {
		log.Println("cleanup checkins:", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var sid, uid int64
		var nick string
		if rows.Scan(&sid, &uid, &nick) == nil {
			h.broadcast("checkout", sid, uid, nick, nil)
		}
	}
}
