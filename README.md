# planka-api

## Configuration

Before starting the API, apply the auth schema:

```sh
psql "$DATABASE_URL" -f migrations/001_auth.sql
```

With Docker Compose the schema is applied automatically when the Postgres volume is created:

```sh
docker compose up --build
```

## Auth endpoints

Planka-compatible endpoints:

- `POST /api/Auth/login` with `{ "email": "user@example.com", "password": "string", "rememberMe": true }`
  returns `{ "accessToken": "...", "refreshToken": "...", "loginSucceeded": true }`
- `POST /api/Auth/refresh` with `{ "refreshToken": "..." }`
  returns `{ "accessToken": "...", "refreshToken": "..." }`
- `POST /api/Auth/logout` with `Authorization: Bearer <accessToken>` and/or `{ "refreshToken": "..." }`
  returns `204 No Content`
- `POST /api/Auth/revoke_all` with `Authorization: Bearer <accessToken>`
  returns `204 No Content`

Existing endpoints:

- `POST /auth/register` with `{ "email": "...", "password": "...", "name": "..." }`
- `POST /auth/login` with `{ "email": "...", "password": "..." }`
- `POST /auth/refresh` with `{ "refresh_token": "..." }`
- `POST /auth/logout` with `Authorization: Bearer <access_token>` and/or `{ "refresh_token": "..." }`
- `GET /auth/me` with `Authorization: Bearer <access_token>`

OAuth2-style endpoints are also available:

- `POST /oauth/token` with form or JSON `grant_type=password`, `username` or `email`, and `password`
- `POST /oauth/token` with form or JSON `grant_type=refresh_token` and `refresh_token`
- `POST /oauth/revoke` with form or JSON `token`
