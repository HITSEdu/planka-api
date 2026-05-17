# planka-api

## Configuration

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
