CREATE TABLE auth_login_attempts (
    email text PRIMARY KEY,
    failed_count integer NOT NULL DEFAULT 0,
    window_started_at timestamptz NOT NULL DEFAULT now(),
    locked_until timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT auth_login_attempts_email_valid CHECK (btrim(email) <> ''),
    CONSTRAINT auth_login_attempts_count_valid CHECK (failed_count >= 0)
);

CREATE INDEX auth_login_attempts_locked_idx ON auth_login_attempts (locked_until)
    WHERE locked_until IS NOT NULL;
