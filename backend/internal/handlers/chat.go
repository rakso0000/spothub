package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakso0000/spothub/backend/internal/auth"
	"github.com/rakso0000/spothub/backend/internal/ws"
)

type ChatHandler struct {
	DB     *pgxpool.Pool
	Hub    *ws.Hub
	Secret []byte
}

type inMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type outMessage struct {
	Type   string    `json:"type"`
	ID     int64     `json:"id"`
	SpotID int64     `json:"spot_id"`
	UserID int64     `json:"user_id"`
	User   string    `json:"user"`
	Text   string    `json:"text"`
	TS     time.Time `json:"ts"`
}

// GET /api/spots/{id}/messages?before=ID&limit=50
// Сообщения идут от новых к старым. before: id самого старого из уже загруженных.
func (h *ChatHandler) History(w http.ResponseWriter, r *http.Request) {
	sid, ok := spotID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id спота")
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 || limit > 100 {
		limit = 50
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT m.id, m.user_id, u.nickname, m.text, m.created_at
		FROM messages m
		JOIN users u ON u.id = m.user_id
		WHERE m.spot_id = $1 AND NOT m.hidden AND ($2::bigint = 0 OR m.id < $2::bigint)
		ORDER BY m.id DESC
		LIMIT $3`, sid, before, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	msgs := []outMessage{}
	for rows.Next() {
		m := outMessage{Type: "message", SpotID: sid}
		if err := rows.Scan(&m.ID, &m.UserID, &m.User, &m.Text, &m.TS); err != nil {
			writeError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		msgs = append(msgs, m)
	}
	writeJSON(w, http.StatusOK, msgs)
}

// GET /ws/spots/{id}?token=...
func (h *ChatHandler) Serve(w http.ResponseWriter, r *http.Request) {
	sid, ok := spotID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "неверный id спота")
		return
	}
	userID, err := auth.ParseToken(h.Secret, r.URL.Query().Get("token"))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "неверный токен")
		return
	}

	var nick string
	err = h.DB.QueryRow(r.Context(), `
		SELECT u.nickname
		FROM users u
		JOIN spots s ON s.id = $2 AND s.status = 'approved'
		WHERE u.id = $1 AND NOT u.banned`, userID, sid).Scan(&nick)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "спот или пользователь не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
	})
	if err != nil {
		return
	}
	conn.SetReadLimit(2048)

	ctx := r.Context()
	client := &ws.Client{SpotID: sid, UserID: userID, Nick: nick, Send: make(chan []byte, 32)}
	h.Hub.Register(client)
	defer h.Hub.Unregister(client)

	// Отправка: читаем из канала и пишем в сокет.
	go func() {
		defer conn.Close(websocket.StatusNormalClosure, "")
		for msg := range client.Send {
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}()

	sendErr := func(text string) {
		data, _ := json.Marshal(map[string]string{"type": "error", "error": text})
		h.Hub.SendTo(client, data)
	}

	// Приём: читаем сообщения от клиента.
	var last time.Time
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		var in inMessage
		if json.Unmarshal(data, &in) != nil || in.Type != "message" {
			sendErr(`ожидается {"type":"message","text":"..."}`)
			continue
		}
		text := strings.TrimSpace(in.Text)
		if n := len([]rune(text)); n == 0 || n > 500 {
			sendErr("сообщение: от 1 до 500 символов")
			continue
		}
		if time.Since(last) < time.Second {
			sendErr("слишком часто, не больше 1 сообщения в секунду")
			continue
		}
		last = time.Now()

		m := outMessage{Type: "message", SpotID: sid, UserID: userID, User: nick, Text: text}
		err = h.DB.QueryRow(ctx, `
			INSERT INTO messages (spot_id, user_id, text)
			VALUES ($1, $2, $3)
			RETURNING id, created_at`, sid, userID, text).Scan(&m.ID, &m.TS)
		if err != nil {
			sendErr("не удалось сохранить сообщение")
			continue
		}

		out, _ := json.Marshal(m)
		h.Hub.Broadcast(sid, out)
	}
}
