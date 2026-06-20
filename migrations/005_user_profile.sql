ALTER TABLE users
    ADD COLUMN IF NOT EXISTS first_name TEXT,
    ADD COLUMN IF NOT EXISTS last_name TEXT,
    ADD COLUMN IF NOT EXISTS patronymic TEXT,
    ADD COLUMN IF NOT EXISTS birth_date DATE,
    ADD COLUMN IF NOT EXISTS gender TEXT NOT NULL DEFAULT 'NotDefined',
    ADD COLUMN IF NOT EXISTS avatar_url TEXT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_gender_check'
          AND conrelid = 'users'::regclass
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_gender_check
            CHECK (gender IN ('Male', 'Female', 'NotDefined'));
    END IF;
END $$;

UPDATE users
SET first_name = NULLIF(name, '')
WHERE first_name IS NULL
  AND NULLIF(name, '') IS NOT NULL;
