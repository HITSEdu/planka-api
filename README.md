# planka-api

## Configuration

```env
APP_ENV=development
HTTP_PORT=8080
DATABASE_URL=postgres://postgres:postgres@localhost:5432/planka_api?sslmode=disable
CORS_ORIGINS=http://localhost:3000,http://localhost:5173
ACCESS_TOKEN_TTL=15m
REFRESH_TOKEN_TTL=720h
```

Before starting the API, apply the auth schema:

```sh
psql "$DATABASE_URL" -f migrations/001_auth.sql
```

## Auth endpoints

- `POST /auth/register` with `{ "email": "...", "password": "...", "name": "..." }`
- `POST /auth/login` with `{ "email": "...", "password": "..." }`
- `POST /auth/refresh` with `{ "refresh_token": "..." }`
- `POST /auth/logout` with `Authorization: Bearer <access_token>` and/or `{ "refresh_token": "..." }`
- `GET /auth/me` with `Authorization: Bearer <access_token>`

OAuth2-style endpoints are also available:

- `POST /oauth/token` with form or JSON `grant_type=password`, `username` or `email`, and `password`
- `POST /oauth/token` with form or JSON `grant_type=refresh_token` and `refresh_token`
- `POST /oauth/revoke` with form or JSON `token`
