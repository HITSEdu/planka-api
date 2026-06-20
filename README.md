# planka-api

## Configuration

Before starting the API, apply the schema:

```sh
psql "$DATABASE_URL" -f migrations/001_auth.sql
psql "$DATABASE_URL" -f migrations/002_schedules.sql
psql "$DATABASE_URL" -f migrations/003_event_model.sql
psql "$DATABASE_URL" -f migrations/004_user_tags.sql
psql "$DATABASE_URL" -f migrations/005_user_profile.sql
```

With Docker Compose the schema is applied automatically when the Postgres volume is created:

```sh
cp .env.example .env
# set POSTGRES_PASSWORD in .env before starting
docker compose up --build
```

For an existing Docker Compose database volume, apply newly added migrations manually, for example:

```sh
docker compose exec -T postgres sh -lc 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f /docker-entrypoint-initdb.d/005_user_profile.sql'
```

## Swagger

After starting the API, open:

```text
http://localhost:8080/swagger/
```

The OpenAPI document is available at:

```text
http://localhost:8080/openapi.json
```

## Auth endpoints

Planka-compatible endpoints:

- `POST /api/Auth/register` with `{ "email": "user@example.com", "password": "string", "name": "User" }`
  returns `{ "accessToken": "...", "refreshToken": "...", "loginSucceeded": true }`
- `POST /api/Auth/login` with `{ "email": "user@example.com", "password": "string", "rememberMe": true }`
  returns `{ "accessToken": "...", "refreshToken": "...", "loginSucceeded": true }`
- `POST /api/Auth/refresh` with `{ "refreshToken": "..." }`
  returns `{ "accessToken": "...", "refreshToken": "..." }`
- `POST /api/Auth/logout` with `Authorization: Bearer <accessToken>` and/or `{ "refreshToken": "..." }`
  returns `204 No Content`
- `POST /api/Auth/revoke_all` with `Authorization: Bearer <accessToken>`
  returns `204 No Content`
- `GET /api/Profile` with `Authorization: Bearer <accessToken>`
  returns current user profile
- `PATCH /api/Profile` with `Authorization: Bearer <accessToken>` and `{ "firstName": "Ivan", "lastName": "Ivanov", "patronymic": "Ivanovich", "birthDate": "2000-01-31", "gender": "Male", "avatarUrl": "https://example.com/avatar.jpg" }`
  updates current user profile

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

## Schedule endpoints

All schedule endpoints require `Authorization: Bearer <access_token>`.

- `GET /schedules`
- `POST /schedules` with `{ "title": "Work" }`
- `GET /schedules/{id}`
- `PATCH /schedules/{id}` with `{ "title": "Updated title" }`
- `DELETE /schedules/{id}`

## Event endpoints

All event endpoints require `Authorization: Bearer <access_token>`.

- `GET /events`
- `GET /events?tag_id=00000000-0000-0000-0000-000000000000`
- `GET /events?tag_name=planning`
- `POST /events` with `{ "title": "Planning", "description": "Sprint planning", "starts_at": "2026-06-16T09:00:00Z", "ends_at": "2026-06-16T10:00:00Z", "focus": 1, "tag_ids": ["00000000-0000-0000-0000-000000000000"] }`
- `GET /events/{id}`
- `PATCH /events/{id}` with the same body as create
- `DELETE /events/{id}`

## Tag endpoints

All tag endpoints require `Authorization: Bearer <access_token>`.

- `GET /tags`
- `POST /tags` with `{ "name": "Planning", "color": "#91FFB5" }`
- `GET /tags/{id}`
- `PATCH /tags/{id}` with `{ "name": "Updated", "color": "#8A91FF" }`
- `DELETE /tags/{id}`

## Event data model

The event schema from the diagram is implemented in `migrations/003_event_model.sql`.
It adds:

- `events` with title, optional description/date range, and focus.
- `tags` with a hex color and many-to-many `event_tags`.
- `migrations/004_user_tags.sql` scopes tags to their owner.
- `event_accesses` with owner, `access_status`, and `event_access_allowed_users`.
- `invitations` with sender, recipient, and `invitation_status`.
