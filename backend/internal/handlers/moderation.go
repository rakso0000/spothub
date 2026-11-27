package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakso0000/spothub/backend/internal/auth"
	"github.com/rakso0000/spothub/backend/internal/ws"
)

// Сколько разных жалоб скрывают сообщение автоматически.
const autoHideThreshold = 3

type ModerationHandler struct {
	DB  *pgxpool.Pool
	Hub *ws.Hub
}

func pathID(r *http.Request) (int64, bool) { return spotID(r) }

// ActiveUser пропускает только незаблокированных пользователей.
func ActiveUser(db *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var banned bool
			err := db.QueryRow(r.Context(),
				`SELECT banned FROM users WHERE id = $1`, auth.UserID(r.Context())).Scan(&banned)
			if err == pgx.ErrNoRows {
				writeError(w, http.StatusUnauthorized, "пользователь не найден")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "ошибка сервера")
				return
			}
			if banned {
				writeError(w, http.StatusForbidden, "аккаунт заблокирован")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireModerator пропускает только модераторов.
func RequireModerator(db *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var ok bool
			err := db.QueryRow(r.Context(),
				`SELECT is_moderator AND NOT banned FROM users WHERE id = $1`,
				auth.UserID(r.Context())).Scan(&ok)
			if err != nil || !ok {
				writeError(w, http.StatusForbidden, "нужны права модератора")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LimitSpotCreation ограничивает число новых спотов на человека в сутки.
func LimitSpotCreation(db *pgxpool.Pool, perDay int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var n int
			err := db.QueryRow(r.Context(), `
				SELECT COUNT(*) FROM spots
				WHERE author_id = $1 AND created_at > now() - interval '24 hours'`,
				auth.UserID(r.Context())).Scan(&n)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "ошибка сервера")
				return
			}
			if n >= perDay {
				writeError(w, http.StatusTooManyRequests, "слишком много новых спотов за сутки")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---------- Жалобы ----------

type reportRequest struct {
	TargetType string `json:"target_type"` // "spot" или "message"
	TargetID   int64  `json:"target_id"`
	Reason     string `json:"reason"`
}

// POST /api/reports (нужен токен)
func (h *ModerationHandler) Report(w http.ResponseWriter, r *http.Request) {
	var req reportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.TargetType != "spot" && req.TargetType != "message" {
		writeError(w, http.StatusBadRequest, "target_type: spot или message")
		return
	}
	if req.TargetID <= 0 {
		writeError(w, http.StatusBadRequest, "неверный target_id")
		return
	}
	if n := len([]rune(req.Reason)); n < 3 || n > 500 {
		writeError(w, http.StatusBadRequest, "причина: от 3 до 500 символов")
		return
	}

	table := "spots"
	if req.TargetType == "message" {
		table = "messages"
	}
	var exists bool
	err := h.DB.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id = $1)`, req.TargetID).Scan(&exists)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "объект не найден")
		return
	}

	_, err = h.DB.Exec(r.Context(), `
		INSERT INTO reports (reporter_id, target_type, target_id, reason)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`,
		auth.UserID(r.Context()), req.TargetType, req.TargetID, req.Reason)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось сохранить жалобу")
		return
	}

	// Сообщения скрываются автоматически, споты ждут решения модератора
	// (иначе спот можно скрыть жалобами с фейковых аккаунтов).
	if req.TargetType == "message" {
		var n int
		err := h.DB.QueryRow(r.Context(), `
			SELECT COUNT(*) FROM reports
			WHERE target_type = 'message' AND target_id = $1 AND NOT resolved`,
			req.TargetID).Scan(&n)
		if err == nil && n >= autoHideThreshold {
			h.hideMessage(r.Context(), req.TargetID)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// hideMessage скрывает сообщение и сообщает об этом участникам чата.
func (h *ModerationHandler) hideMessage(ctx context.Context, id int64) (bool, error) {
	var sid int64
	err := h.DB.QueryRow(ctx,
		`UPDATE messages SET hidden = true WHERE id = $1 AND NOT hidden RETURNING spot_id`, id).Scan(&sid)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	data, _ := json.Marshal(map[string]any{"type": "message_hidden", "id": id, "spot_id": sid})
	h.Hub.Broadcast(sid, data)
	return true, nil
}

// ---------- Модераторские ручки (/api/admin/...) ----------

type reportItem struct {
	ID           int64     `json:"id"`
	TargetType   string    `json:"target_type"`
	TargetID     int64     `json:"target_id"`
	TargetUserID *int64    `json:"target_user_id"`
	Preview      string    `json:"preview"`
	Reason       string    `json:"reason"`
	Reporter     string    `json:"reporter"`
	CreatedAt    time.Time `json:"created_at"`
}

// GET /api/admin/reports
func (h *ModerationHandler) ListReports(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(r.Context(), `
		SELECT r.id, r.target_type, r.target_id, COALESCE(m.user_id, s.author_id),
		       COALESCE(m.text, s.title, ''), r.reason, u.nickname, r.created_at
		FROM reports r
		JOIN users u ON u.id = r.reporter_id
		LEFT JOIN messages m ON r.target_type = 'message' AND m.id = r.target_id
		LEFT JOIN spots s ON r.target_type = 'spot' AND s.id = r.target_id
		WHERE NOT r.resolved
		ORDER BY r.created_at
		LIMIT 100`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	list := []reportItem{}
	for rows.Next() {
		var it reportItem
		if err := rows.Scan(&it.ID, &it.TargetType, &it.TargetID, &it.TargetUserID,
			&it.Preview, &it.Reason, &it.Reporter, &it.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		list = append(list, it)
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/admin/reports/{id}/resolve
func (h *ModerationHandler) ResolveReport(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id")
		return
	}
	tag, err := h.DB.Exec(r.Context(),
		`UPDATE reports SET resolved = true WHERE id = $1 AND NOT resolved`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "жалоба не найдена или уже закрыта")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /api/admin/spots/pending
func (h *ModerationHandler) PendingSpots(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(r.Context(), `
		SELECT id, title, description,
		       ST_Y(location::geometry), ST_X(location::geometry), sports
		FROM spots
		WHERE status = 'pending'
		ORDER BY id
		LIMIT 100`)
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

// POST /api/admin/spots/{id}/approve
func (h *ModerationHandler) ApproveSpot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id")
		return
	}
	tag, err := h.DB.Exec(r.Context(),
		`UPDATE spots SET status = 'approved' WHERE id = $1 AND status <> 'approved'`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "спот не найден или уже одобрен")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/admin/spots/{id}/hide
func (h *ModerationHandler) HideSpot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id")
		return
	}
	tag, err := h.DB.Exec(r.Context(),
		`UPDATE spots SET status = 'hidden' WHERE id = $1 AND status <> 'hidden'`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "спот не найден или уже скрыт")
		return
	}
	h.DB.Exec(r.Context(),
		`UPDATE reports SET resolved = true WHERE target_type = 'spot' AND target_id = $1`, id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/admin/messages/{id}/hide
func (h *ModerationHandler) HideMessage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id")
		return
	}
	hidden, err := h.hideMessage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	if !hidden {
		writeError(w, http.StatusNotFound, "сообщение не найдено или уже скрыто")
		return
	}
	h.DB.Exec(r.Context(),
		`UPDATE reports SET resolved = true WHERE target_type = 'message' AND target_id = $1`, id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/admin/users/{id}/ban
func (h *ModerationHandler) Ban(w http.ResponseWriter, r *http.Request) {
	uid, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id")
		return
	}
	var nick string
	err := h.DB.QueryRow(r.Context(),
		`UPDATE users SET banned = true WHERE id = $1 AND NOT is_moderator RETURNING nickname`, uid).Scan(&nick)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "пользователь не найден или это модератор")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}

	h.Hub.KickUser(uid) // закрываем его WebSocket-соединения

	// Убираем из «Я здесь» и сообщаем комнатам.
	rows, err := h.DB.Query(r.Context(),
		`DELETE FROM checkins WHERE user_id = $1 RETURNING spot_id`, uid)
	if err == nil {
		var spots []int64
		for rows.Next() {
			var sid int64
			if rows.Scan(&sid) == nil {
				spots = append(spots, sid)
			}
		}
		rows.Close()
		for _, sid := range spots {
			data, _ := json.Marshal(presenceEvent{
				Type: "checkout", SpotID: sid, UserID: uid, User: nick, TS: time.Now().UTC(),
			})
			h.Hub.Broadcast(sid, data)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/admin/users/{id}/unban
func (h *ModerationHandler) Unban(w http.ResponseWriter, r *http.Request) {
	uid, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id")
		return
	}
	tag, err := h.DB.Exec(r.Context(), `UPDATE users SET banned = false WHERE id = $1`, uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "пользователь не найден")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
