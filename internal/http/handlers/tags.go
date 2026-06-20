package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var tagColorPattern = regexp.MustCompile(`^#([0-9A-Fa-f]{3}|[0-9A-Fa-f]{6})$`)

type Tags struct {
	db *sql.DB
}

func NewTags(db *sql.DB) *Tags {
	return &Tags{db: db}
}

type tagResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type tagRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type tagUpdateRequest struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

func (t *Tags) List(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(t.db, w, r)
	if !ok {
		return
	}

	rows, err := t.db.QueryContext(
		r.Context(),
		`SELECT id::text, name, color, created_at, updated_at
		 FROM tags
		 WHERE user_id = $1
		 ORDER BY lower(name), created_at`,
		user.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list tags")
		return
	}
	defer rows.Close()

	tags := make([]tagResponse, 0)
	for rows.Next() {
		tag, err := scanTag(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read tags")
			return
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not read tags")
		return
	}

	writeJSON(w, http.StatusOK, tags)
}

func (t *Tags) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(t.db, w, r)
	if !ok {
		return
	}

	var req tagRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	name, color, ok := normalizeTagFields(w, req.Name, req.Color)
	if !ok {
		return
	}

	tag, err := scanTag(t.db.QueryRowContext(
		r.Context(),
		`INSERT INTO tags (user_id, name, color)
		 VALUES ($1, $2, $3)
		 RETURNING id::text, name, color, created_at, updated_at`,
		user.ID,
		name,
		color,
	))
	if err != nil {
		writeError(w, http.StatusConflict, "tag already exists")
		return
	}

	writeJSON(w, http.StatusCreated, tag)
}

func (t *Tags) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(t.db, w, r)
	if !ok {
		return
	}

	tagID, ok := tagIDFromRequest(w, r)
	if !ok {
		return
	}

	tag, ok := t.findOwnedTag(w, r, user.ID, tagID)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, tag)
}

func (t *Tags) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(t.db, w, r)
	if !ok {
		return
	}

	tagID, ok := tagIDFromRequest(w, r)
	if !ok {
		return
	}

	var req tagUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	name, color, ok := normalizeTagUpdateFields(w, req)
	if !ok {
		return
	}

	tag, err := scanTag(t.db.QueryRowContext(
		r.Context(),
		`UPDATE tags
		 SET name = COALESCE($1, name),
		     color = COALESCE($2, color),
		     updated_at = now()
		 WHERE id = $3 AND user_id = $4
		 RETURNING id::text, name, color, created_at, updated_at`,
		name,
		color,
		tagID,
		user.ID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tag not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, "tag already exists")
		return
	}

	writeJSON(w, http.StatusOK, tag)
}

func (t *Tags) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(t.db, w, r)
	if !ok {
		return
	}

	tagID, ok := tagIDFromRequest(w, r)
	if !ok {
		return
	}

	result, err := t.db.ExecContext(
		r.Context(),
		`DELETE FROM tags WHERE id = $1 AND user_id = $2`,
		tagID,
		user.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete tag")
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete tag")
		return
	}
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, "tag not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (t *Tags) findOwnedTag(w http.ResponseWriter, r *http.Request, userID, tagID string) (tagResponse, bool) {
	tag, err := scanTag(t.db.QueryRowContext(
		r.Context(),
		`SELECT id::text, name, color, created_at, updated_at
		 FROM tags
		 WHERE id = $1 AND user_id = $2`,
		tagID,
		userID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tag not found")
		return tagResponse{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load tag")
		return tagResponse{}, false
	}

	return tag, true
}

func normalizeTagFields(w http.ResponseWriter, name, color string) (string, string, bool) {
	name = strings.TrimSpace(name)
	color = strings.TrimSpace(color)

	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return "", "", false
	}
	if !tagColorPattern.MatchString(color) {
		writeError(w, http.StatusBadRequest, "valid hex color is required")
		return "", "", false
	}

	return name, color, true
}

func normalizeTagUpdateFields(w http.ResponseWriter, req tagUpdateRequest) (*string, *string, bool) {
	if req.Name == nil && req.Color == nil {
		writeError(w, http.StatusBadRequest, "name or color is required")
		return nil, nil, false
	}

	var name *string
	if req.Name != nil {
		value := strings.TrimSpace(*req.Name)
		if value == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return nil, nil, false
		}
		name = &value
	}

	var color *string
	if req.Color != nil {
		value := strings.TrimSpace(*req.Color)
		if !tagColorPattern.MatchString(value) {
			writeError(w, http.StatusBadRequest, "valid hex color is required")
			return nil, nil, false
		}
		color = &value
	}

	return name, color, true
}

func scanTag(scanner eventScanner) (tagResponse, error) {
	var tag tagResponse
	err := scanner.Scan(&tag.ID, &tag.Name, &tag.Color, &tag.CreatedAt, &tag.UpdatedAt)
	if err != nil {
		return tagResponse{}, err
	}

	return tag, nil
}

func tagIDFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "valid tag id is required")
		return "", false
	}

	return id, true
}
