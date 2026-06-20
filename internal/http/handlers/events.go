package handlers

import (
	"context"
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
	ID            string        `json:"id"`
	Title         string        `json:"title"`
	Description   *string       `json:"description"`
	StartsAt      *time.Time    `json:"starts_at"`
	EndsAt        *time.Time    `json:"ends_at"`
	Focus         float64       `json:"focus"`
	AccessStatus  string        `json:"access_status"`
	SharedUserIDs []string      `json:"shared_user_ids"`
	Tags          []tagResponse `json:"tags"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type eventRequest struct {
	Title         string     `json:"title"`
	Description   *string    `json:"description"`
	StartsAt      *time.Time `json:"starts_at"`
	EndsAt        *time.Time `json:"ends_at"`
	Focus         float64    `json:"focus"`
	AccessStatus  string     `json:"access_status"`
	TagIDs        []string   `json:"tag_ids"`
	SharedUserIDs []string   `json:"shared_user_ids"`
}

type normalizedEventRequest struct {
	Title         string
	Description   *string
	StartsAt      *time.Time
	EndsAt        *time.Time
	Focus         float64
	AccessStatus  string
	TagIDs        []string
	SharedUserIDs []string
}

type eventTagFilter struct {
	ID   string
	Name string
}

type eventScanner interface {
	Scan(dest ...any) error
}

type eventTagQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type eventQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

var errEventTagNotFound = errors.New("event tag not found")
var errEventSharedUserNotFriend = errors.New("event shared user is not an accepted friend")
var errEventSharedUsersRequired = errors.New("event shared users are required")

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
		         AND t.user_id = $1
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
		if err := e.loadTagsForEvent(r.Context(), &event); err != nil {
			writeError(w, http.StatusInternalServerError, "could not read event tags")
			return
		}
		if err := loadEventSharedUserIDs(r.Context(), e.db, &event); err != nil {
			writeError(w, http.StatusInternalServerError, "could not read event sharing")
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

	tx, err := e.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start event creation")
		return
	}
	defer rollback(tx)

	var eventID string
	err = tx.QueryRowContext(
		r.Context(),
		`INSERT INTO events (title, description, starts_at, ends_at, focus)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id::text`,
		req.Title,
		req.Description,
		req.StartsAt,
		req.EndsAt,
		req.Focus,
	).Scan(&eventID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create event")
		return
	}

	var accessID string
	err = tx.QueryRowContext(
		r.Context(),
		`INSERT INTO event_accesses (event_id, owner_id, status)
		 VALUES ($1, $2, $3)
		 RETURNING id::text`,
		eventID,
		user.ID,
		req.AccessStatus,
	).Scan(&accessID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create event access")
		return
	}

	if err := replaceEventTags(r.Context(), tx, user.ID, eventID, req.TagIDs); errors.Is(err, errEventTagNotFound) {
		writeError(w, http.StatusBadRequest, "all tag_ids must belong to current user")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set event tags")
		return
	}

	if err := replaceEventSharedUsers(r.Context(), tx, user.ID, accessID, req.AccessStatus, req.SharedUserIDs); errors.Is(err, errEventSharedUsersRequired) {
		writeError(w, http.StatusBadRequest, "shared_user_ids are required for shared events")
		return
	} else if errors.Is(err, errEventSharedUserNotFriend) {
		writeError(w, http.StatusBadRequest, "all shared_user_ids must be accepted friends")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set event sharing")
		return
	}

	event, err := loadOwnedEvent(r.Context(), tx, user.ID, eventID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load created event")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not commit event creation")
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

	tx, err := e.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start event update")
		return
	}
	defer rollback(tx)

	var accessID string
	err = tx.QueryRowContext(
		r.Context(),
		`SELECT id::text
		 FROM event_accesses
		 WHERE event_id = $1
		   AND owner_id = $2
		 FOR UPDATE`,
		eventID,
		user.ID,
	).Scan(&accessID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update event")
		return
	}

	result, err := tx.ExecContext(
		r.Context(),
		`UPDATE events
		 SET title = $1,
		     description = $2,
		     starts_at = $3,
		     ends_at = $4,
		     focus = $5,
		     updated_at = now()
		 WHERE id = $6`,
		req.Title,
		req.Description,
		req.StartsAt,
		req.EndsAt,
		req.Focus,
		eventID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update event")
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update event")
		return
	}
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}

	_, err = tx.ExecContext(
		r.Context(),
		`UPDATE event_accesses
		 SET status = $1,
		     updated_at = now()
		 WHERE id = $2`,
		req.AccessStatus,
		accessID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update event")
		return
	}

	if err := replaceEventTags(r.Context(), tx, user.ID, eventID, req.TagIDs); errors.Is(err, errEventTagNotFound) {
		writeError(w, http.StatusBadRequest, "all tag_ids must belong to current user")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set event tags")
		return
	}

	if err := replaceEventSharedUsers(r.Context(), tx, user.ID, accessID, req.AccessStatus, req.SharedUserIDs); errors.Is(err, errEventSharedUsersRequired) {
		writeError(w, http.StatusBadRequest, "shared_user_ids are required for shared events")
		return
	} else if errors.Is(err, errEventSharedUserNotFriend) {
		writeError(w, http.StatusBadRequest, "all shared_user_ids must be accepted friends")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set event sharing")
		return
	}

	event, err := loadOwnedEvent(r.Context(), tx, user.ID, eventID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load updated event")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not commit event update")
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
	event, err := loadOwnedEvent(r.Context(), e.db, userID, eventID)
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

func (e *Events) loadTagsForEvent(ctx context.Context, event *eventResponse) error {
	return loadEventTags(ctx, e.db, event)
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

	tagIDs := uniqueStrings(req.TagIDs)
	for _, tagID := range tagIDs {
		if !isUUID(tagID) {
			writeError(w, http.StatusBadRequest, "valid tag_ids are required")
			return normalizedEventRequest{}, false
		}
	}

	accessStatus := strings.TrimSpace(req.AccessStatus)
	if accessStatus == "" {
		accessStatus = "PRIVATE"
	}
	if accessStatus != "PRIVATE" && accessStatus != "PUBLIC" && accessStatus != "SHARED" {
		writeError(w, http.StatusBadRequest, "valid access_status is required")
		return normalizedEventRequest{}, false
	}

	sharedUserIDs := uniqueStrings(req.SharedUserIDs)
	for _, sharedUserID := range sharedUserIDs {
		if !isUUID(sharedUserID) {
			writeError(w, http.StatusBadRequest, "valid shared_user_ids are required")
			return normalizedEventRequest{}, false
		}
	}
	if accessStatus == "SHARED" && len(sharedUserIDs) == 0 {
		writeError(w, http.StatusBadRequest, "shared_user_ids are required for shared events")
		return normalizedEventRequest{}, false
	}

	return normalizedEventRequest{
		Title:         title,
		Description:   trimOptionalString(req.Description),
		StartsAt:      req.StartsAt,
		EndsAt:        req.EndsAt,
		Focus:         req.Focus,
		AccessStatus:  accessStatus,
		TagIDs:        tagIDs,
		SharedUserIDs: sharedUserIDs,
	}, true
}

func replaceEventTags(ctx context.Context, tx *sql.Tx, userID, eventID string, tagIDs []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM event_tags WHERE event_id = $1`, eventID); err != nil {
		return err
	}

	for _, tagID := range tagIDs {
		result, err := tx.ExecContext(
			ctx,
			`INSERT INTO event_tags (event_id, tag_id)
			 SELECT $1, id
			 FROM tags
			 WHERE id = $2 AND user_id = $3`,
			eventID,
			tagID,
			userID,
		)
		if err != nil {
			return err
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return errEventTagNotFound
		}
	}

	return nil
}

func replaceEventSharedUsers(ctx context.Context, tx *sql.Tx, userID, eventAccessID, accessStatus string, sharedUserIDs []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM event_access_allowed_users WHERE event_access_id = $1`, eventAccessID); err != nil {
		return err
	}

	if accessStatus != "SHARED" {
		return nil
	}

	normalizedSharedUserIDs := make([]string, 0, len(sharedUserIDs))
	for _, sharedUserID := range sharedUserIDs {
		if sharedUserID == userID {
			continue
		}
		normalizedSharedUserIDs = append(normalizedSharedUserIDs, sharedUserID)
	}
	if len(normalizedSharedUserIDs) == 0 {
		return errEventSharedUsersRequired
	}

	for _, sharedUserID := range normalizedSharedUserIDs {
		result, err := tx.ExecContext(
			ctx,
			`INSERT INTO event_access_allowed_users (event_access_id, user_id)
			 SELECT $1, u.id
			 FROM users u
			 WHERE u.id = $2
			   AND EXISTS (
			       SELECT 1
			       FROM invitations i
			       WHERE i.status = 'ACCEPTED'
			         AND (
			             (i.from_user_id = $3 AND i.to_user_id = u.id)
			             OR (i.from_user_id = u.id AND i.to_user_id = $3)
			         )
			   )`,
			eventAccessID,
			sharedUserID,
			userID,
		)
		if err != nil {
			return err
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return errEventSharedUserNotFriend
		}
	}

	return nil
}

func loadEventTags(ctx context.Context, queryer eventTagQueryer, event *eventResponse) error {
	rows, err := queryer.QueryContext(
		ctx,
		`SELECT t.id::text, t.name, t.color, t.created_at, t.updated_at
		 FROM event_tags et
		 JOIN tags t ON t.id = et.tag_id
		 WHERE et.event_id = $1
		 ORDER BY lower(t.name), t.created_at`,
		event.ID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	tags := make([]tagResponse, 0)
	for rows.Next() {
		tag, err := scanTag(rows)
		if err != nil {
			return err
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	event.Tags = tags
	return nil
}

func loadEventSharedUserIDs(ctx context.Context, queryer eventTagQueryer, event *eventResponse) error {
	rows, err := queryer.QueryContext(
		ctx,
		`SELECT eau.user_id::text
		 FROM event_access_allowed_users eau
		 JOIN event_accesses ea ON ea.id = eau.event_access_id
		 WHERE ea.event_id = $1
		 ORDER BY eau.created_at`,
		event.ID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	sharedUserIDs := make([]string, 0)
	for rows.Next() {
		var sharedUserID string
		if err := rows.Scan(&sharedUserID); err != nil {
			return err
		}
		sharedUserIDs = append(sharedUserIDs, sharedUserID)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	event.SharedUserIDs = sharedUserIDs
	return nil
}

func loadOwnedEvent(ctx context.Context, queryer eventQueryer, userID, eventID string) (eventResponse, error) {
	event, err := scanEvent(queryer.QueryRowContext(
		ctx,
		eventSelectSQL+`
		 WHERE e.id = $1 AND a.owner_id = $2`,
		eventID,
		userID,
	))
	if err != nil {
		return eventResponse{}, err
	}
	if err := loadEventTags(ctx, queryer, &event); err != nil {
		return eventResponse{}, err
	}
	if err := loadEventSharedUserIDs(ctx, queryer, &event); err != nil {
		return eventResponse{}, err
	}

	return event, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result
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

	event.SharedUserIDs = make([]string, 0)

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
