package handlers

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
)

type Events struct {
	db *sql.DB
}

func NewEvents(db *sql.DB) *Events {
	return &Events{db: db}
}

type eventResponse struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Description  *string    `json:"description"`
	StartsAt     *time.Time `json:"starts_at"`
	EndsAt       *time.Time `json:"ends_at"`
	Focus        float64    `json:"focus"`
	AccessStatus string     `json:"access_status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type eventRequest struct {
	Title       string     `json:"title"`
	Description *string    `json:"description"`
	StartsAt    *time.Time `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	Focus       float64    `json:"focus"`
}

type normalizedEventRequest struct {
	Title       string
	Description *string
	StartsAt    *time.Time
	EndsAt      *time.Time
	Focus       float64
}

type eventTagFilter struct {
	ID   string
	Name string
}

type eventScanner interface {
	Scan(dest ...any) error
}

func (e *Events) List(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(e.db, w, r)
	if !ok {
		return
	}

	tagFilter, ok := eventTagFilterFromRequest(w, r)
	if !ok {
		return
	}

	query := eventSelectSQL + `
		 WHERE a.owner_id = $1`
	args := []any{user.ID}

	if tagFilter.ID != "" {
		query += `
		   AND EXISTS (
		       SELECT 1
		       FROM event_tags et
		       WHERE et.event_id = e.id
		         AND et.tag_id = $2
		   )`
		args = append(args, tagFilter.ID)
	}
	if tagFilter.Name != "" {
		query += `
		   AND EXISTS (
		       SELECT 1
		       FROM event_tags et
		       JOIN tags t ON t.id = et.tag_id
		       WHERE et.event_id = e.id
		         AND t.name = $2
		   )`
		args = append(args, tagFilter.Name)
	}

	query += `
		 ORDER BY COALESCE(e.starts_at, e.created_at) DESC, e.created_at DESC`

	rows, err := e.db.QueryContext(
		r.Context(),
		query,
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list events")
		return
	}
	defer rows.Close()

	events := make([]eventResponse, 0)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read events")
			return
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not read events")
		return
	}

	writeJSON(w, http.StatusOK, events)
}

func (e *Events) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(e.db, w, r)
	if !ok {
		return
	}

	req, ok := decodeEventRequest(w, r)
	if !ok {
		return
	}

	event, err := scanEvent(e.db.QueryRowContext(
		r.Context(),
		`WITH created_event AS (
			 INSERT INTO events (title, description, starts_at, ends_at, focus)
			 VALUES ($1, $2, $3, $4, $5)
			 RETURNING id, title, description, starts_at, ends_at, focus, created_at, updated_at
		 ), created_access AS (
			 INSERT INTO event_accesses (event_id, owner_id)
			 SELECT id, $6 FROM created_event
			 RETURNING event_id, status
		 )
		 SELECT ce.id::text, ce.title, ce.description, ce.starts_at, ce.ends_at, ce.focus,
		        ca.status::text, ce.created_at, ce.updated_at
		 FROM created_event ce
		 JOIN created_access ca ON ca.event_id = ce.id`,
		req.Title,
		req.Description,
		req.StartsAt,
		req.EndsAt,
		req.Focus,
		user.ID,
	))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create event")
		return
	}

	writeJSON(w, http.StatusCreated, event)
}

func (e *Events) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(e.db, w, r)
	if !ok {
		return
	}

	eventID, ok := eventIDFromRequest(w, r)
	if !ok {
		return
	}

	event, ok := e.findOwnedEvent(w, r, user.ID, eventID)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, event)
}

func (e *Events) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(e.db, w, r)
	if !ok {
		return
	}

	req, ok := decodeEventRequest(w, r)
	if !ok {
		return
	}

	eventID, ok := eventIDFromRequest(w, r)
	if !ok {
		return
	}

	event, err := scanEvent(e.db.QueryRowContext(
		r.Context(),
		`UPDATE events e
		 SET title = $1,
		     description = $2,
		     starts_at = $3,
		     ends_at = $4,
		     focus = $5,
		     updated_at = now()
		 FROM event_accesses a
		 WHERE e.id = $6
		   AND a.event_id = e.id
		   AND a.owner_id = $7
		 RETURNING e.id::text, e.title, e.description, e.starts_at, e.ends_at, e.focus,
		           a.status::text, e.created_at, e.updated_at`,
		req.Title,
		req.Description,
		req.StartsAt,
		req.EndsAt,
		req.Focus,
		eventID,
		user.ID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update event")
		return
	}

	writeJSON(w, http.StatusOK, event)
}

func (e *Events) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(e.db, w, r)
	if !ok {
		return
	}

	eventID, ok := eventIDFromRequest(w, r)
	if !ok {
		return
	}

	result, err := e.db.ExecContext(
		r.Context(),
		`DELETE FROM events e
		 USING event_accesses a
		 WHERE e.id = $1
		   AND a.event_id = e.id
		   AND a.owner_id = $2`,
		eventID,
		user.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete event")
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete event")
		return
	}
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (e *Events) findOwnedEvent(w http.ResponseWriter, r *http.Request, userID, eventID string) (eventResponse, bool) {
	event, err := scanEvent(e.db.QueryRowContext(
		r.Context(),
		eventSelectSQL+`
		 WHERE e.id = $1 AND a.owner_id = $2`,
		eventID,
		userID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "event not found")
		return eventResponse{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load event")
		return eventResponse{}, false
	}

	return event, true
}

func decodeEventRequest(w http.ResponseWriter, r *http.Request) (normalizedEventRequest, bool) {
	var req eventRequest
	if !decodeJSON(w, r, &req) {
		return normalizedEventRequest{}, false
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return normalizedEventRequest{}, false
	}

	if math.IsNaN(req.Focus) || math.IsInf(req.Focus, 0) {
		writeError(w, http.StatusBadRequest, "focus must be finite")
		return normalizedEventRequest{}, false
	}

	if req.StartsAt != nil && req.EndsAt != nil && req.StartsAt.After(*req.EndsAt) {
		writeError(w, http.StatusBadRequest, "starts_at must be before or equal to ends_at")
		return normalizedEventRequest{}, false
	}

	return normalizedEventRequest{
		Title:       title,
		Description: trimOptionalString(req.Description),
		StartsAt:    req.StartsAt,
		EndsAt:      req.EndsAt,
		Focus:       req.Focus,
	}, true
}

func eventTagFilterFromRequest(w http.ResponseWriter, r *http.Request) (eventTagFilter, bool) {
	tagID := strings.TrimSpace(r.URL.Query().Get("tag_id"))
	tagName := strings.TrimSpace(r.URL.Query().Get("tag_name"))

	if tagID != "" && tagName != "" {
		writeError(w, http.StatusBadRequest, "only one tag filter is allowed")
		return eventTagFilter{}, false
	}

	if tagID != "" && !isUUID(tagID) {
		writeError(w, http.StatusBadRequest, "valid tag_id is required")
		return eventTagFilter{}, false
	}

	return eventTagFilter{
		ID:   tagID,
		Name: tagName,
	}, true
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func scanEvent(scanner eventScanner) (eventResponse, error) {
	var event eventResponse
	var description sql.NullString
	var startsAt sql.NullTime
	var endsAt sql.NullTime

	err := scanner.Scan(
		&event.ID,
		&event.Title,
		&description,
		&startsAt,
		&endsAt,
		&event.Focus,
		&event.AccessStatus,
		&event.CreatedAt,
		&event.UpdatedAt,
	)
	if err != nil {
		return eventResponse{}, err
	}

	if description.Valid {
		value := description.String
		event.Description = &value
	}
	if startsAt.Valid {
		value := startsAt.Time
		event.StartsAt = &value
	}
	if endsAt.Valid {
		value := endsAt.Time
		event.EndsAt = &value
	}

	return event, nil
}

func eventIDFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "valid event id is required")
		return "", false
	}

	return id, true
}

const eventSelectSQL = `SELECT e.id::text, e.title, e.description, e.starts_at, e.ends_at, e.focus,
       a.status::text, e.created_at, e.updated_at
FROM events e
JOIN event_accesses a ON a.event_id = e.id`
