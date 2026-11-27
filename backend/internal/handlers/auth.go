package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/rakso0000/spothub/backend/internal/auth"
)

type AuthHandler struct {
	DB     *pgxpool.Pool
	Secret []byte
}

type authRequest struct {
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string `json:"token"`
	ID    int64  `json:"id"`
	Nick  string `json:"nickname"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	req.Nickname = strings.TrimSpace(req.Nickname)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	if n := len([]rune(req.Nickname)); n < 3 || n > 30 {
		writeError(w, http.StatusBadRequest, "ник: от 3 до 30 символов")
		return
	}
	if !strings.Contains(req.Email, "@") {
		writeError(w, http.StatusBadRequest, "неверный email")
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 72 {
		writeError(w, http.StatusBadRequest, "пароль: от 8 до 72 символов")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}

	var id int64
	err = h.DB.QueryRow(r.Context(),
		`INSERT INTO users (nickname, email, password_hash) VALUES ($1,$2,$3) RETURNING id`,
		req.Nickname, req.Email, string(hash)).Scan(&id)
	if err != nil {
		writeError(w, http.StatusConflict, "ник или email уже заняты")
		return
	}

	token, err := auth.NewToken(h.Secret, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	writeJSON(w, http.StatusCreated, authResponse{Token: token, ID: id, Nick: req.Nickname})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))

	var id int64
	var nick, hash string
	err := h.DB.QueryRow(r.Context(),
		`SELECT id, nickname, password_hash FROM users WHERE email=$1`, email).
		Scan(&id, &nick, &hash)
	if err == pgx.ErrNoRows ||
		(err == nil && bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil) {
		writeError(w, http.StatusUnauthorized, "неверный email или пароль")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}

	token, err := auth.NewToken(h.Secret, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	writeJSON(w, http.StatusOK, authResponse{Token: token, ID: id, Nick: nick})
}
