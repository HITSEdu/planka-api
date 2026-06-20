ALTER TABLE tags
    ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE CASCADE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.table_constraints
        WHERE table_schema = 'public'
          AND table_name = 'tags'
          AND constraint_name = 'tags_name_key'
    ) THEN
        ALTER TABLE tags DROP CONSTRAINT tags_name_key;
    END IF;
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM tags WHERE user_id IS NULL) THEN
        ALTER TABLE tags ALTER COLUMN user_id SET NOT NULL;
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS tags_user_id_idx ON tags(user_id);
CREATE UNIQUE INDEX IF NOT EXISTS tags_user_id_name_unique_idx
    ON tags(user_id, lower(name))
    WHERE user_id IS NOT NULL;
