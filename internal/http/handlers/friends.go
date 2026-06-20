package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

type Friends struct {
	db *sql.DB
}

func NewFriends(db *sql.DB) *Friends {
	return &Friends{db: db}
}

type friendUserResponse struct {
	ID        string  `json:"id"`
	Email     string  `json:"email"`
	Name      *string `json:"name"`
	AvatarURL *string `json:"avatar_url"`
}

type friendResponse struct {
	ID                string  `json:"id"`
	Email             string  `json:"email"`
	Name              *string `json:"name"`
	AvatarURL         *string `json:"avatar_url"`
	SharedEventsCount int     `json:"shared_events_count"`
}

type friendInvitationResponse struct {
	ID        string             `json:"id"`
	User      friendUserResponse `json:"user"`
	CreatedAt time.Time          `json:"created_at"`
}

type friendsOverviewResponse struct {
	Friends          []friendResponse           `json:"friends"`
	IncomingRequests []friendInvitationResponse `json:"incoming_requests"`
	OutgoingRequests []friendInvitationResponse `json:"outgoing_requests"`
}

type createFriendRequestRequest struct {
	Email string `json:"email"`
}

type invitationRecord struct {
	ID         string
	FromUserID string
	ToUserID   string
	Status     string
}

func (f *Friends) List(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(f.db, w, r)
	if !ok {
		return
	}

	friends, err := listFriends(r.Context(), f.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list friends")
		return
	}

	incoming, err := listIncomingFriendRequests(r.Context(), f.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list incoming requests")
		return
	}

	outgoing, err := listOutgoingFriendRequests(r.Context(), f.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list outgoing requests")
		return
	}

	writeJSON(w, http.StatusOK, friendsOverviewResponse{
		Friends:          friends,
		IncomingRequests: incoming,
		OutgoingRequests: outgoing,
	})
}

func (f *Friends) CreateRequest(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(f.db, w, r)
	if !ok {
		return
	}

	var req createFriendRequestRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email := normalizeEmail(req.Email)
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	target, found, err := findUserByEmail(r.Context(), f.db, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if target.ID == user.ID {
		writeError(w, http.StatusBadRequest, "cannot add yourself as a friend")
		return
	}

	existingInvitation, err := findInvitationBetween(r.Context(), f.db, user.ID, target.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create friend request")
		return
	}
	if existingInvitation != nil {
		switch existingInvitation.Status {
		case "ACCEPTED":
			writeError(w, http.StatusConflict, "users are already friends")
		case "PENDING":
			if existingInvitation.FromUserID == user.ID {
				writeError(w, http.StatusConflict, "friend request already sent")
			} else {
				writeError(w, http.StatusConflict, "incoming friend request already exists")
			}
		default:
			writeError(w, http.StatusConflict, "friend request already exists")
		}
		return
	}

	var invitationID string
	var createdAt time.Time
	err = f.db.QueryRowContext(
		r.Context(),
		`INSERT INTO invitations (from_user_id, to_user_id)
		 VALUES ($1, $2)
		 RETURNING id::text, created_at`,
		user.ID,
		target.ID,
	).Scan(&invitationID, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create friend request")
		return
	}

	writeJSON(w, http.StatusCreated, friendInvitationResponse{
		ID:        invitationID,
		User:      target,
		CreatedAt: createdAt,
	})
}

func (f *Friends) AcceptRequest(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(f.db, w, r)
	if !ok {
		return
	}

	requestID, ok := invitationIDFromRequest(w, r)
	if !ok {
		return
	}

	friend, err := scanFriend(f.db.QueryRowContext(
		r.Context(),
		`UPDATE invitations i
		 SET status = 'ACCEPTED',
		     updated_at = now()
		 FROM users u
		 WHERE i.id = $1
		   AND i.to_user_id = $2
		   AND i.status = 'PENDING'
		   AND u.id = i.from_user_id
		 RETURNING u.id::text,
		           u.email,
		           NULLIF(u.name, ''),
		           NULLIF(u.avatar_url, ''),
		           (
		               SELECT COUNT(*)::int
		               FROM event_accesses ea
		               JOIN event_access_allowed_users eau ON eau.event_access_id = ea.id
		               WHERE ea.owner_id = u.id
		                 AND ea.status = 'SHARED'
		                 AND eau.user_id = $2
		           )`,
		requestID,
		user.ID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "friend request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not accept friend request")
		return
	}

	writeJSON(w, http.StatusOK, friend)
}

func (f *Friends) DeleteRequest(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(f.db, w, r)
	if !ok {
		return
	}

	requestID, ok := invitationIDFromRequest(w, r)
	if !ok {
		return
	}

	result, err := f.db.ExecContext(
		r.Context(),
		`DELETE FROM invitations
		 WHERE id = $1
		   AND status = 'PENDING'
		   AND (from_user_id = $2 OR to_user_id = $2)`,
		requestID,
		user.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete friend request")
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete friend request")
		return
	}
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, "friend request not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (f *Friends) Remove(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(f.db, w, r)
	if !ok {
		return
	}

	friendID, ok := friendIDFromRequest(w, r)
	if !ok {
		return
	}

	tx, err := f.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove friend")
		return
	}
	defer rollback(tx)

	result, err := tx.ExecContext(
		r.Context(),
		`DELETE FROM invitations
		 WHERE status = 'ACCEPTED'
		   AND (
		       (from_user_id = $1 AND to_user_id = $2)
		       OR (from_user_id = $2 AND to_user_id = $1)
		   )`,
		user.ID,
		friendID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove friend")
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove friend")
		return
	}
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, "friend not found")
		return
	}

	if err := cleanupFriendEventSharesTx(r.Context(), tx, user.ID, friendID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove friend")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove friend")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (f *Friends) ListSharedEvents(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromAccessToken(f.db, w, r)
	if !ok {
		return
	}

	friendID, ok := friendIDFromRequest(w, r)
	if !ok {
		return
	}

	isFriend, err := acceptedFriendshipExists(r.Context(), f.db, user.ID, friendID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list friend events")
		return
	}
	if !isFriend {
		writeError(w, http.StatusNotFound, "friend not found")
		return
	}

	tagFilter, ok := eventTagFilterFromRequest(w, r)
	if !ok {
		return
	}

	query := eventSelectSQL + `
		 WHERE a.owner_id = $1
		   AND (
		       a.status = 'PUBLIC'
		       OR (
		           a.status = 'SHARED'
		           AND EXISTS (
		               SELECT 1
		               FROM event_access_allowed_users eau
		               WHERE eau.event_access_id = a.id
		                 AND eau.user_id = $2
		           )
		       )
		   )`
	args := []any{friendID, user.ID}

	if tagFilter.ID != "" {
		query += `
		   AND EXISTS (
		       SELECT 1
		       FROM event_tags et
		       WHERE et.event_id = e.id
		         AND et.tag_id = $3
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
		         AND t.name = $3
		   )`
		args = append(args, tagFilter.Name)
	}

	query += `
		 ORDER BY COALESCE(e.starts_at, e.created_at) DESC, e.created_at DESC`

	rows, err := f.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list friend events")
		return
	}
	defer rows.Close()

	events := make([]eventResponse, 0)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read friend events")
			return
		}
		if err := loadEventTags(r.Context(), f.db, &event); err != nil {
			writeError(w, http.StatusInternalServerError, "could not read friend event tags")
			return
		}
		event.SharedUserIDs = make([]string, 0)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not read friend events")
		return
	}

	writeJSON(w, http.StatusOK, events)
}

func findUserByEmail(ctx context.Context, db *sql.DB, email string) (friendUserResponse, bool, error) {
	var user friendUserResponse
	var name sql.NullString
	var avatarURL sql.NullString

	err := db.QueryRowContext(
		ctx,
		`SELECT id::text, email, NULLIF(name, ''), NULLIF(avatar_url, '')
		 FROM users
		 WHERE email = $1`,
		email,
	).Scan(&user.ID, &user.Email, &name, &avatarURL)
	if errors.Is(err, sql.ErrNoRows) {
		return friendUserResponse{}, false, nil
	}
	if err != nil {
		return friendUserResponse{}, false, err
	}

	if name.Valid {
		value := name.String
		user.Name = &value
	}
	if avatarURL.Valid {
		value := avatarURL.String
		user.AvatarURL = &value
	}

	return user, true, nil
}

func findInvitationBetween(ctx context.Context, db *sql.DB, userID, otherUserID string) (*invitationRecord, error) {
	var invitation invitationRecord

	err := db.QueryRowContext(
		ctx,
		`SELECT id::text, from_user_id::text, to_user_id::text, status::text
		 FROM invitations
		 WHERE (from_user_id = $1 AND to_user_id = $2)
		    OR (from_user_id = $2 AND to_user_id = $1)
		 ORDER BY
		     CASE status
		         WHEN 'ACCEPTED' THEN 0
		         ELSE 1
		     END,
		     created_at DESC
		 LIMIT 1`,
		userID,
		otherUserID,
	).Scan(&invitation.ID, &invitation.FromUserID, &invitation.ToUserID, &invitation.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &invitation, nil
}

func listFriends(ctx context.Context, db *sql.DB, userID string) ([]friendResponse, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT u.id::text,
		        u.email,
		        NULLIF(u.name, ''),
		        NULLIF(u.avatar_url, ''),
		        (
		            SELECT COUNT(*)::int
		            FROM event_accesses ea
		            JOIN event_access_allowed_users eau ON eau.event_access_id = ea.id
		            WHERE ea.owner_id = u.id
		              AND ea.status = 'SHARED'
		              AND eau.user_id = $1
		        ) AS shared_events_count
		 FROM invitations i
		 JOIN users u ON u.id = CASE
		     WHEN i.from_user_id = $1 THEN i.to_user_id
		     ELSE i.from_user_id
		 END
		 WHERE i.status = 'ACCEPTED'
		   AND (i.from_user_id = $1 OR i.to_user_id = $1)
		 ORDER BY lower(COALESCE(NULLIF(u.name, ''), u.email)), i.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	friends := make([]friendResponse, 0)
	for rows.Next() {
		friend, err := scanFriend(rows)
		if err != nil {
			return nil, err
		}
		friends = append(friends, friend)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return friends, nil
}

func listIncomingFriendRequests(ctx context.Context, db *sql.DB, userID string) ([]friendInvitationResponse, error) {
	return listFriendRequests(
		ctx,
		db,
		`SELECT i.id::text, u.id::text, u.email, NULLIF(u.name, ''), NULLIF(u.avatar_url, ''), i.created_at
		 FROM invitations i
		 JOIN users u ON u.id = i.from_user_id
		 WHERE i.to_user_id = $1
		   AND i.status = 'PENDING'
		 ORDER BY i.created_at DESC`,
		userID,
	)
}

func listOutgoingFriendRequests(ctx context.Context, db *sql.DB, userID string) ([]friendInvitationResponse, error) {
	return listFriendRequests(
		ctx,
		db,
		`SELECT i.id::text, u.id::text, u.email, NULLIF(u.name, ''), NULLIF(u.avatar_url, ''), i.created_at
		 FROM invitations i
		 JOIN users u ON u.id = i.to_user_id
		 WHERE i.from_user_id = $1
		   AND i.status = 'PENDING'
		 ORDER BY i.created_at DESC`,
		userID,
	)
}

func listFriendRequests(ctx context.Context, db *sql.DB, query, userID string) ([]friendInvitationResponse, error) {
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	requests := make([]friendInvitationResponse, 0)
	for rows.Next() {
		request, err := scanFriendInvitation(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return requests, nil
}

func acceptedFriendshipExists(ctx context.Context, db *sql.DB, userID, otherUserID string) (bool, error) {
	var exists bool

	err := db.QueryRowContext(
		ctx,
		`SELECT EXISTS (
		     SELECT 1
		     FROM invitations
		     WHERE status = 'ACCEPTED'
		       AND (
		           (from_user_id = $1 AND to_user_id = $2)
		           OR (from_user_id = $2 AND to_user_id = $1)
		       )
		 )`,
		userID,
		otherUserID,
	).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func cleanupFriendEventSharesTx(ctx context.Context, tx *sql.Tx, userID, friendID string) error {
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM event_access_allowed_users eau
		 USING event_accesses ea
		 WHERE eau.event_access_id = ea.id
		   AND (
		       (ea.owner_id = $1 AND eau.user_id = $2)
		       OR (ea.owner_id = $2 AND eau.user_id = $1)
		   )`,
		userID,
		friendID,
	); err != nil {
		return err
	}

	_, err := tx.ExecContext(
		ctx,
		`UPDATE event_accesses ea
		 SET status = 'PRIVATE',
		     updated_at = now()
		 WHERE ea.status = 'SHARED'
		   AND ea.owner_id IN ($1, $2)
		   AND NOT EXISTS (
		       SELECT 1
		       FROM event_access_allowed_users eau
		       WHERE eau.event_access_id = ea.id
		   )`,
		userID,
		friendID,
	)
	return err
}

func friendIDFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "valid friend id is required")
		return "", false
	}

	return id, true
}

func invitationIDFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "valid invitation id is required")
		return "", false
	}

	return id, true
}

type friendScanner interface {
	Scan(dest ...any) error
}

func scanFriend(scanner friendScanner) (friendResponse, error) {
	var friend friendResponse
	var name sql.NullString
	var avatarURL sql.NullString

	err := scanner.Scan(&friend.ID, &friend.Email, &name, &avatarURL, &friend.SharedEventsCount)
	if err != nil {
		return friendResponse{}, err
	}

	if name.Valid {
		value := name.String
		friend.Name = &value
	}
	if avatarURL.Valid {
		value := avatarURL.String
		friend.AvatarURL = &value
	}

	return friend, nil
}

func scanFriendInvitation(scanner friendScanner) (friendInvitationResponse, error) {
	var invitation friendInvitationResponse
	var name sql.NullString
	var avatarURL sql.NullString

	err := scanner.Scan(
		&invitation.ID,
		&invitation.User.ID,
		&invitation.User.Email,
		&name,
		&avatarURL,
		&invitation.CreatedAt,
	)
	if err != nil {
		return friendInvitationResponse{}, err
	}

	if name.Valid {
		value := name.String
		invitation.User.Name = &value
	}
	if avatarURL.Valid {
		value := avatarURL.String
		invitation.User.AvatarURL = &value
	}

	return invitation, nil
}
