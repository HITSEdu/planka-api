package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

type Schedules struct {
	db *sql.DB
}

func NewSchedules(db *sql.DB) *Schedules {
	return &Schedules{db: db}
}

type scheduleResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type scheduleRequest struct {
	Title string `json:"title"`
}

func (s *Schedules) List(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFromAccessToken(w, r)
	if !ok {
		return
	}

	rows, err := s.db.QueryContext(
		r.Context(),
		`SELECT id::text, title, created_at, updated_at
		 FROM schedules
		 WHERE user_id = $1
		 ORDER BY created_at DESC`,
		user.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list schedules")
		return
	}
	defer rows.Close()

	schedules := make([]scheduleResponse, 0)
	for rows.Next() {
		var schedule scheduleResponse
		if err := rows.Scan(&schedule.ID, &schedule.Title, &schedule.CreatedAt, &schedule.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "could not read schedules")
			return
		}
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not read schedules")
		return
	}

	writeJSON(w, http.StatusOK, schedules)
}

func (s *Schedules) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFromAccessToken(w, r)
	if !ok {
		return
	}

	var req scheduleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	var schedule scheduleResponse
	err := s.db.QueryRowContext(
		r.Context(),
		`INSERT INTO schedules (user_id, title)
		 VALUES ($1, $2)
		 RETURNING id::text, title, created_at, updated_at`,
		user.ID,
		title,
	).Scan(&schedule.ID, &schedule.Title, &schedule.CreatedAt, &schedule.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create schedule")
		return
	}

	writeJSON(w, http.StatusCreated, schedule)
}

func (s *Schedules) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFromAccessToken(w, r)
	if !ok {
		return
	}

	scheduleID, ok := scheduleIDFromRequest(w, r)
	if !ok {
		return
	}

	schedule, ok := s.findOwnedSchedule(w, r, user.ID, scheduleID)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, schedule)
}

func (s *Schedules) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFromAccessToken(w, r)
	if !ok {
		return
	}

	var req scheduleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	scheduleID, ok := scheduleIDFromRequest(w, r)
	if !ok {
		return
	}

	var schedule scheduleResponse
	err := s.db.QueryRowContext(
		r.Context(),
		`UPDATE schedules
		 SET title = $1, updated_at = now()
		 WHERE id = $2 AND user_id = $3
		 RETURNING id::text, title, created_at, updated_at`,
		title,
		scheduleID,
		user.ID,
	).Scan(&schedule.ID, &schedule.Title, &schedule.CreatedAt, &schedule.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update schedule")
		return
	}

	writeJSON(w, http.StatusOK, schedule)
}

func (s *Schedules) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFromAccessToken(w, r)
	if !ok {
		return
	}

	scheduleID, ok := scheduleIDFromRequest(w, r)
	if !ok {
		return
	}

	result, err := s.db.ExecContext(
		r.Context(),
		`DELETE FROM schedules WHERE id = $1 AND user_id = $2`,
		scheduleID,
		user.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete schedule")
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete schedule")
		return
	}
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Schedules) findOwnedSchedule(w http.ResponseWriter, r *http.Request, userID, scheduleID string) (scheduleResponse, bool) {
	var schedule scheduleResponse
	err := s.db.QueryRowContext(
		r.Context(),
		`SELECT id::text, title, created_at, updated_at
		 FROM schedules
		 WHERE id = $1 AND user_id = $2`,
		scheduleID,
		userID,
	).Scan(&schedule.ID, &schedule.Title, &schedule.CreatedAt, &schedule.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "schedule not found")
		return scheduleResponse{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load schedule")
		return scheduleResponse{}, false
	}

	return schedule, true
}

func (s *Schedules) userFromAccessToken(w http.ResponseWriter, r *http.Request) (userResponse, bool) {
	accessToken := bearerToken(r)
	if accessToken == "" {
		writeError(w, http.StatusUnauthorized, "bearer token is required")
		return userResponse{}, false
	}

	var user userResponse
	err := s.db.QueryRowContext(
		r.Context(),
		`SELECT u.id::text, u.email, COALESCE(u.name, ''), u.created_at
		 FROM auth_tokens t
		 JOIN users u ON u.id = t.user_id
		 WHERE t.access_token_hash = $1
		   AND t.revoked_at IS NULL
		   AND t.access_expires_at > now()`,
		tokenHash(accessToken),
	).Scan(&user.ID, &user.Email, &user.Name, &user.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid access token")
		return userResponse{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user")
		return userResponse{}, false
	}

	return user, true
}

func scheduleIDFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "valid schedule id is required")
		return "", false
	}

	return id, true
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}

	for i, char := range value {
		switch i {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !isHex(char) {
				return false
			}
		}
	}

	return true
}

func isHex(char rune) bool {
	return char >= '0' && char <= '9' ||
		char >= 'a' && char <= 'f' ||
		char >= 'A' && char <= 'F'
}
