DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'access_status') THEN
        CREATE TYPE access_status AS ENUM ('PUBLIC', 'PRIVATE', 'SHARED');
    END IF;
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'invitation_status') THEN
        CREATE TYPE invitation_status AS ENUM ('PENDING', 'ACCEPTED');
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    description TEXT,
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    focus DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT events_title_not_blank_check CHECK (length(btrim(title)) > 0),
    CONSTRAINT events_focus_finite_check CHECK (focus <> 'NaN'::double precision),
    CONSTRAINT events_date_range_check CHECK (starts_at IS NULL OR ends_at IS NULL OR starts_at <= ends_at)
);

CREATE TABLE IF NOT EXISTS tags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    color TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tags_name_not_blank_check CHECK (length(btrim(name)) > 0),
    CONSTRAINT tags_color_hex_check CHECK (color ~ '^#([0-9A-Fa-f]{3}|[0-9A-Fa-f]{6})$')
);

CREATE TABLE IF NOT EXISTS event_accesses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL UNIQUE REFERENCES events(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status access_status NOT NULL DEFAULT 'PRIVATE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS event_access_allowed_users (
    event_access_id UUID NOT NULL REFERENCES event_accesses(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (event_access_id, user_id)
);

CREATE TABLE IF NOT EXISTS event_tags (
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, tag_id)
);

CREATE TABLE IF NOT EXISTS invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    to_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status invitation_status NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT invitations_distinct_users_check CHECK (from_user_id <> to_user_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS invitations_from_to_unique_idx ON invitations(from_user_id, to_user_id);
CREATE INDEX IF NOT EXISTS invitations_from_user_id_idx ON invitations(from_user_id);
CREATE INDEX IF NOT EXISTS invitations_to_user_id_idx ON invitations(to_user_id);
CREATE INDEX IF NOT EXISTS invitations_status_idx ON invitations(status);

CREATE INDEX IF NOT EXISTS events_starts_at_idx ON events(starts_at);
CREATE INDEX IF NOT EXISTS events_ends_at_idx ON events(ends_at);

CREATE INDEX IF NOT EXISTS event_accesses_owner_id_idx ON event_accesses(owner_id);
CREATE INDEX IF NOT EXISTS event_accesses_status_idx ON event_accesses(status);

CREATE INDEX IF NOT EXISTS event_access_allowed_users_user_id_idx ON event_access_allowed_users(user_id);
CREATE INDEX IF NOT EXISTS event_tags_tag_id_idx ON event_tags(tag_id);
