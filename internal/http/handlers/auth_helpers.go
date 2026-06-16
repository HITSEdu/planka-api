package handlers

import (
	"database/sql"
	"errors"
	"net/http"
)

func currentUserFromAccessToken(db *sql.DB, w http.ResponseWriter, r *http.Request) (userResponse, bool) {
	accessToken := bearerToken(r)
	if accessToken == "" {
		writeError(w, http.StatusUnauthorized, "bearer token is required")
		return userResponse{}, false
	}

	var user userResponse
	err := db.QueryRowContext(
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
