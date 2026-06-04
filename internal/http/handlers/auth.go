package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	passwordSaltBytes = 16
	passwordKeyBytes  = 32
	passwordRounds    = 210000
	tokenBytes        = 32
)

type Auth struct {
	db              *sql.DB
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

func NewAuth(db *sql.DB, accessTokenTTL, refreshTokenTTL time.Duration) *Auth {
	return &Auth{
		db:              db,
		accessTokenTTL:  accessTokenTTL,
		refreshTokenTTL: refreshTokenTTL,
	}
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type apiAuthLoginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	RememberMe bool   `json:"rememberMe"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type apiAuthRefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type apiAuthLogoutRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type oauthTokenRequest struct {
	GrantType    string `json:"grant_type"`
	Email        string `json:"email"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	RefreshToken string `json:"refresh_token"`
}

type userResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type tokenResponse struct {
	TokenType             string       `json:"token_type"`
	AccessToken           string       `json:"access_token"`
	ExpiresIn             int64        `json:"expires_in"`
	RefreshToken          string       `json:"refresh_token"`
	RefreshTokenExpiresIn int64        `json:"refresh_token_expires_in"`
	User                  userResponse `json:"user"`
}

type apiAuthLoginResponse struct {
	AccessToken    string `json:"accessToken"`
	RefreshToken   string `json:"refreshToken"`
	LoginSucceeded bool   `json:"loginSucceeded"`
}

type apiAuthRefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

type apiProfileResponse struct {
	ID         string  `json:"id"`
	Email      string  `json:"email"`
	LastName   *string `json:"lastName"`
	FirstName  *string `json:"firstName"`
	Patronymic *string `json:"patronymic"`
	BirthDate  string  `json:"birthDate"`
	Gender     string  `json:"gender"`
}

func (a *Auth) Register(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email := normalizeEmail(req.Email)
	if email == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "email and password with at least 8 characters are required")
		return
	}

	passwordHash, err := hashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	var user userResponse
	err = a.db.QueryRowContext(
		r.Context(),
		`INSERT INTO users (email, password_hash, name)
		 VALUES ($1, $2, NULLIF($3, ''))
		 RETURNING id::text, email, COALESCE(name, ''), created_at`,
		email,
		passwordHash,
		strings.TrimSpace(req.Name),
	).Scan(&user.ID, &user.Email, &user.Name, &user.CreatedAt)
	if err != nil {
		writeError(w, http.StatusConflict, "user already exists")
		return
	}

	a.writeTokenPair(w, r.Context(), user, http.StatusCreated)
}

func (a *Auth) APIRegister(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email := normalizeEmail(req.Email)
	if email == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "email and password with at least 8 characters are required")
		return
	}

	passwordHash, err := hashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	var user userResponse
	err = a.db.QueryRowContext(
		r.Context(),
		`INSERT INTO users (email, password_hash, name)
		 VALUES ($1, $2, NULLIF($3, ''))
		 RETURNING id::text, email, COALESCE(name, ''), created_at`,
		email,
		passwordHash,
		strings.TrimSpace(req.Name),
	).Scan(&user.ID, &user.Email, &user.Name, &user.CreatedAt)
	if err != nil {
		writeError(w, http.StatusConflict, "user already exists")
		return
	}

	pair, err := a.createTokenPair(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create tokens")
		return
	}

	writeJSON(w, http.StatusCreated, apiAuthLoginResponse{
		AccessToken:    pair.AccessToken,
		RefreshToken:   pair.RefreshToken,
		LoginSucceeded: true,
	})
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	user, ok := a.authenticatePassword(w, r.Context(), req.Email, req.Password)
	if !ok {
		return
	}

	a.writeTokenPair(w, r.Context(), user, http.StatusOK)
}

func (a *Auth) APILogin(w http.ResponseWriter, r *http.Request) {
	var req apiAuthLoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	user, ok := a.authenticatePassword(w, r.Context(), req.Email, req.Password)
	if !ok {
		return
	}

	pair, err := a.createTokenPair(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create tokens")
		return
	}

	writeJSON(w, http.StatusOK, apiAuthLoginResponse{
		AccessToken:    pair.AccessToken,
		RefreshToken:   pair.RefreshToken,
		LoginSucceeded: true,
	})
}

func (a *Auth) Token(w http.ResponseWriter, r *http.Request) {
	req, ok := parseOAuthTokenRequest(w, r)
	if !ok {
		return
	}

	switch req.GrantType {
	case "password":
		email := req.Email
		if email == "" {
			email = req.Username
		}

		user, ok := a.authenticatePassword(w, r.Context(), email, req.Password)
		if !ok {
			return
		}

		a.writeTokenPair(w, r.Context(), user, http.StatusOK)
	case "refresh_token":
		pair, user, ok := a.rotateRefreshToken(w, r.Context(), req.RefreshToken)
		if !ok {
			return
		}

		writeJSON(w, http.StatusOK, a.tokenResponse(pair, user))
	default:
		writeError(w, http.StatusBadRequest, "unsupported grant_type")
	}
}

func (a *Auth) Revoke(w http.ResponseWriter, r *http.Request) {
	token := ""

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			Token string `json:"token"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		token = req.Token
	} else {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form body")
			return
		}
		token = r.FormValue("token")
	}

	token = strings.TrimSpace(token)
	if token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	hash := tokenHash(token)
	_, _ = a.db.ExecContext(
		r.Context(),
		`UPDATE auth_tokens
		 SET revoked_at = now()
		 WHERE access_token_hash = $1 OR refresh_token_hash = $1`,
		hash,
	)

	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) authenticatePassword(w http.ResponseWriter, ctx context.Context, email, password string) (userResponse, bool) {
	email = normalizeEmail(email)
	if email == "" || password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return userResponse{}, false
	}

	var user userResponse
	var passwordHash string
	err := a.db.QueryRowContext(
		ctx,
		`SELECT id::text, email, COALESCE(name, ''), created_at, password_hash
		 FROM users
		 WHERE email = $1`,
		email,
	).Scan(&user.ID, &user.Email, &user.Name, &user.CreatedAt, &passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return userResponse{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user")
		return userResponse{}, false
	}
	if !verifyPassword(password, passwordHash) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return userResponse{}, false
	}

	return user, true
}

func (a *Auth) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	pair, user, ok := a.rotateRefreshToken(w, r.Context(), refreshToken)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, a.tokenResponse(pair, user))
}

func (a *Auth) APIRefresh(w http.ResponseWriter, r *http.Request) {
	var req apiAuthRefreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		writeError(w, http.StatusBadRequest, "refreshToken is required")
		return
	}

	pair, _, ok := a.rotateRefreshToken(w, r.Context(), refreshToken)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, apiAuthRefreshResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	})
}

func (a *Auth) rotateRefreshToken(w http.ResponseWriter, ctx context.Context, refreshToken string) (tokenPair, userResponse, bool) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return tokenPair{}, userResponse{}, false
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start refresh")
		return tokenPair{}, userResponse{}, false
	}
	defer rollback(tx)

	var user userResponse
	var tokenID int64
	err = tx.QueryRowContext(
		ctx,
		`SELECT t.id, u.id::text, u.email, COALESCE(u.name, ''), u.created_at
		 FROM auth_tokens t
		 JOIN users u ON u.id = t.user_id
		 WHERE t.refresh_token_hash = $1
		   AND t.revoked_at IS NULL
		   AND t.refresh_expires_at > now()
		 FOR UPDATE`,
		tokenHash(refreshToken),
	).Scan(&tokenID, &user.ID, &user.Email, &user.Name, &user.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
		return tokenPair{}, userResponse{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not refresh token")
		return tokenPair{}, userResponse{}, false
	}

	if _, err := tx.ExecContext(ctx, `UPDATE auth_tokens SET revoked_at = now() WHERE id = $1`, tokenID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not revoke refresh token")
		return tokenPair{}, userResponse{}, false
	}

	pair, err := a.createTokenPairTx(ctx, tx, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create tokens")
		return tokenPair{}, userResponse{}, false
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not commit refresh")
		return tokenPair{}, userResponse{}, false
	}

	return pair, user, true
}

func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	var req logoutRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	accessToken := bearerToken(r)
	if accessToken == "" && strings.TrimSpace(req.RefreshToken) == "" {
		writeError(w, http.StatusBadRequest, "access token or refresh_token is required")
		return
	}

	if accessToken != "" {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE auth_tokens SET revoked_at = now() WHERE access_token_hash = $1`, tokenHash(accessToken))
	}
	if strings.TrimSpace(req.RefreshToken) != "" {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE auth_tokens SET revoked_at = now() WHERE refresh_token_hash = $1`, tokenHash(req.RefreshToken))
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) APILogout(w http.ResponseWriter, r *http.Request) {
	var req apiAuthLogoutRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	accessToken := bearerToken(r)
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if accessToken == "" && refreshToken == "" {
		writeError(w, http.StatusBadRequest, "access token or refreshToken is required")
		return
	}

	if accessToken != "" {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE auth_tokens SET revoked_at = now() WHERE access_token_hash = $1`, tokenHash(accessToken))
	}
	if refreshToken != "" {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE auth_tokens SET revoked_at = now() WHERE refresh_token_hash = $1`, tokenHash(refreshToken))
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) APIRevokeAll(w http.ResponseWriter, r *http.Request) {
	user, ok := a.userFromAccessToken(w, r)
	if !ok {
		return
	}

	if _, err := a.db.ExecContext(
		r.Context(),
		`UPDATE auth_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
		user.ID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not revoke tokens")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := a.userFromAccessToken(w, r)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (a *Auth) APIProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := a.userFromAccessToken(w, r)
	if !ok {
		return
	}

	var firstName *string
	if user.Name != "" {
		firstName = &user.Name
	}

	writeJSON(w, http.StatusOK, apiProfileResponse{
		ID:        user.ID,
		Email:     user.Email,
		FirstName: firstName,
		BirthDate: "",
		Gender:    "NotDefined",
	})
}

func (a *Auth) userFromAccessToken(w http.ResponseWriter, r *http.Request) (userResponse, bool) {
	accessToken := bearerToken(r)
	if accessToken == "" {
		writeError(w, http.StatusUnauthorized, "bearer token is required")
		return userResponse{}, false
	}

	var user userResponse
	err := a.db.QueryRowContext(
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

func (a *Auth) writeTokenPair(w http.ResponseWriter, ctx context.Context, user userResponse, status int) {
	pair, err := a.createTokenPair(ctx, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create tokens")
		return
	}

	writeJSON(w, status, a.tokenResponse(pair, user))
}

type tokenPair struct {
	AccessToken           string
	RefreshToken          string
	ExpiresIn             int64
	RefreshTokenExpiresIn int64
}

func (a *Auth) createTokenPair(ctx context.Context, userID string) (tokenPair, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return tokenPair{}, err
	}
	defer rollback(tx)

	pair, err := a.createTokenPairTx(ctx, tx, userID)
	if err != nil {
		return tokenPair{}, err
	}

	if err := tx.Commit(); err != nil {
		return tokenPair{}, err
	}

	return pair, nil
}

func (a *Auth) createTokenPairTx(ctx context.Context, tx *sql.Tx, userID string) (tokenPair, error) {
	accessToken, err := randomString(tokenBytes)
	if err != nil {
		return tokenPair{}, err
	}

	refreshToken, err := randomString(tokenBytes)
	if err != nil {
		return tokenPair{}, err
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO auth_tokens (user_id, access_token_hash, refresh_token_hash, access_expires_at, refresh_expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		userID,
		tokenHash(accessToken),
		tokenHash(refreshToken),
		time.Now().UTC().Add(a.accessTokenTTL),
		time.Now().UTC().Add(a.refreshTokenTTL),
	)
	if err != nil {
		return tokenPair{}, err
	}

	return tokenPair{
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		ExpiresIn:             int64(a.accessTokenTTL.Seconds()),
		RefreshTokenExpiresIn: int64(a.refreshTokenTTL.Seconds()),
	}, nil
}

func (a *Auth) tokenResponse(pair tokenPair, user userResponse) tokenResponse {
	return tokenResponse{
		TokenType:             "Bearer",
		AccessToken:           pair.AccessToken,
		ExpiresIn:             pair.ExpiresIn,
		RefreshToken:          pair.RefreshToken,
		RefreshTokenExpiresIn: pair.RefreshTokenExpiresIn,
		User:                  user,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return false
	}

	return true
}

func parseOAuthTokenRequest(w http.ResponseWriter, r *http.Request) (oauthTokenRequest, bool) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var req oauthTokenRequest
		if !decodeJSON(w, r, &req) {
			return oauthTokenRequest{}, false
		}

		req.GrantType = strings.TrimSpace(req.GrantType)
		req.Email = strings.TrimSpace(req.Email)
		req.Username = strings.TrimSpace(req.Username)
		req.RefreshToken = strings.TrimSpace(req.RefreshToken)
		return req, true
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form body")
		return oauthTokenRequest{}, false
	}

	return oauthTokenRequest{
		GrantType:    strings.TrimSpace(r.FormValue("grant_type")),
		Email:        strings.TrimSpace(firstNonEmpty(r.FormValue("email"), r.FormValue("username"))),
		Username:     strings.TrimSpace(r.FormValue("username")),
		Password:     r.FormValue("password"),
		RefreshToken: strings.TrimSpace(r.FormValue("refresh_token")),
	}, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	prefix := "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}

	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomString(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashPassword(password string) (string, error) {
	salt, err := randomString(passwordSaltBytes)
	if err != nil {
		return "", err
	}

	saltBytes, err := base64.RawURLEncoding.DecodeString(salt)
	if err != nil {
		return "", err
	}

	key := pbkdf2Key([]byte(password), saltBytes, passwordRounds, passwordKeyBytes)
	return strings.Join([]string{
		"pbkdf2_sha256",
		fmt.Sprintf("%d", passwordRounds),
		salt,
		base64.RawURLEncoding.EncodeToString(key),
	}, "$"), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" || parts[1] != fmt.Sprintf("%d", passwordRounds) {
		return false
	}

	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}

	want, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}

	got := pbkdf2Key([]byte(password), salt, passwordRounds, len(want))
	return hmac.Equal(got, want)
}

func pbkdf2Key(password, salt []byte, iter, keyLen int) []byte {
	hashLen := sha256.Size
	numBlocks := (keyLen + hashLen - 1) / hashLen
	var key []byte

	for block := 1; block <= numBlocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)

		for i := 1; i < iter; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}

		key = append(key, t...)
	}

	return key[:keyLen]
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}
