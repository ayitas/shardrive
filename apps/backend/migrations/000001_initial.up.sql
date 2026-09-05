CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_not_blank CHECK (btrim(email) <> ''),
    CONSTRAINT users_password_hash_not_blank CHECK (btrim(password_hash) <> '')
);

CREATE TABLE directories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id uuid REFERENCES directories(id) ON DELETE RESTRICT,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT directories_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT directories_not_own_parent CHECK (parent_id IS NULL OR parent_id <> id)
);

CREATE UNIQUE INDEX directories_sibling_name_unique
    ON directories (user_id, parent_id, name) NULLS NOT DISTINCT;
CREATE INDEX directories_user_parent_idx ON directories (user_id, parent_id);

CREATE TABLE storage_accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    provider text NOT NULL,
    status text NOT NULL DEFAULT 'ACTIVE',
    total_bytes bigint NOT NULL,
    used_bytes bigint NOT NULL DEFAULT 0,
    free_bytes bigint GENERATED ALWAYS AS (total_bytes - used_bytes) STORED,
    priority integer NOT NULL DEFAULT 0,
    max_upload_workers integer NOT NULL DEFAULT 2,
    max_download_workers integer NOT NULL DEFAULT 1,
    credential_ref text,
    rate_limited_until timestamptz,
    last_health_check timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT storage_accounts_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT storage_accounts_provider_not_blank CHECK (btrim(provider) <> ''),
    CONSTRAINT storage_accounts_status_valid CHECK (status IN (
        'ACTIVE', 'DEGRADED', 'FULL', 'OFFLINE', 'RATE_LIMITED',
        'AUTH_FAILED', 'DISABLED', 'AUTH_REQUIRED'
    )),
    CONSTRAINT storage_accounts_capacity_valid CHECK (
        total_bytes >= 0 AND used_bytes >= 0 AND used_bytes <= total_bytes
    ),
    CONSTRAINT storage_accounts_workers_valid CHECK (
        max_upload_workers > 0 AND max_download_workers > 0
    ),
    UNIQUE (user_id, name)
);

CREATE INDEX storage_accounts_user_status_idx ON storage_accounts (user_id, status);
CREATE INDEX storage_accounts_status_rate_limit_idx
    ON storage_accounts (status, rate_limited_until);

CREATE TABLE files (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    directory_id uuid REFERENCES directories(id) ON DELETE SET NULL,
    name text NOT NULL,
    mime_type text NOT NULL DEFAULT 'application/octet-stream',
    size_bytes bigint NOT NULL,
    chunk_size bigint NOT NULL,
    chunk_count integer NOT NULL,
    checksum_sha256 varchar(64),
    state text NOT NULL DEFAULT 'UPLOADING',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT files_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT files_size_valid CHECK (size_bytes >= 0),
    CONSTRAINT files_chunk_size_valid CHECK (chunk_size > 0),
    CONSTRAINT files_chunk_count_valid CHECK (chunk_count >= 0),
    CONSTRAINT files_checksum_valid CHECK (
        checksum_sha256 IS NULL OR checksum_sha256 ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT files_state_valid CHECK (state IN (
        'UPLOADING', 'VERIFYING', 'AVAILABLE', 'DEGRADED',
        'DELETING', 'DELETED', 'FAILED'
    ))
);

CREATE INDEX files_user_directory_state_idx ON files (user_id, directory_id, state);
CREATE INDEX files_user_deleted_at_idx ON files (user_id, deleted_at);

CREATE TABLE upload_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    file_id uuid NOT NULL UNIQUE REFERENCES files(id) ON DELETE CASCADE,
    expected_size bigint NOT NULL,
    received_bytes bigint NOT NULL DEFAULT 0,
    expected_chunks integer NOT NULL,
    completed_chunks integer NOT NULL DEFAULT 0,
    state text NOT NULL DEFAULT 'CREATING',
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT upload_sessions_bytes_valid CHECK (
        expected_size >= 0 AND received_bytes >= 0 AND received_bytes <= expected_size
    ),
    CONSTRAINT upload_sessions_chunks_valid CHECK (
        expected_chunks >= 0 AND completed_chunks >= 0
        AND completed_chunks <= expected_chunks
    ),
    CONSTRAINT upload_sessions_state_valid CHECK (state IN (
        'CREATING', 'UPLOADING', 'VERIFYING', 'COMPLETED',
        'EXPIRED', 'CANCELLED', 'FAILED'
    ))
);

CREATE INDEX upload_sessions_user_state_idx ON upload_sessions (user_id, state);
CREATE INDEX upload_sessions_expiry_idx
    ON upload_sessions (expires_at) WHERE state NOT IN ('COMPLETED', 'EXPIRED', 'CANCELLED');

CREATE TABLE chunks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    chunk_index integer NOT NULL,
    size_bytes bigint NOT NULL,
    checksum_sha256 varchar(64),
    storage_account_id uuid REFERENCES storage_accounts(id) ON DELETE RESTRICT,
    remote_object_id text,
    remote_path text,
    state text NOT NULL DEFAULT 'PENDING',
    retry_count integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chunks_index_valid CHECK (chunk_index >= 0),
    CONSTRAINT chunks_size_valid CHECK (size_bytes >= 0),
    CONSTRAINT chunks_checksum_valid CHECK (
        checksum_sha256 IS NULL OR checksum_sha256 ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chunks_state_valid CHECK (state IN (
        'PENDING', 'UPLOADING', 'STORED', 'CORRUPT',
        'DELETING', 'DELETED', 'FAILED'
    )),
    CONSTRAINT chunks_retry_count_valid CHECK (retry_count >= 0),
    CONSTRAINT chunks_stored_mapping_required CHECK (
        state <> 'STORED' OR (
            checksum_sha256 IS NOT NULL
            AND storage_account_id IS NOT NULL
            AND remote_object_id IS NOT NULL
            AND btrim(remote_object_id) <> ''
        )
    ),
    UNIQUE (file_id, chunk_index)
);

CREATE INDEX chunks_file_order_idx ON chunks (file_id, chunk_index);
CREATE INDEX chunks_account_state_idx ON chunks (storage_account_id, state);
CREATE UNIQUE INDEX chunks_remote_object_unique
    ON chunks (storage_account_id, remote_object_id)
    WHERE storage_account_id IS NOT NULL AND remote_object_id IS NOT NULL;

CREATE TABLE jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid REFERENCES users(id) ON DELETE CASCADE,
    type text NOT NULL,
    state text NOT NULL DEFAULT 'PENDING',
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    attempts integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 5,
    run_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    locked_by text,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CONSTRAINT jobs_type_valid CHECK (type IN (
        'CLEANUP_UPLOAD', 'DELETE_FILE', 'DELETE_ORPHAN',
        'REFRESH_ACCOUNT_QUOTA', 'ACCOUNT_HEALTH_CHECK'
    )),
    CONSTRAINT jobs_state_valid CHECK (state IN (
        'PENDING', 'RUNNING', 'COMPLETED', 'FAILED'
    )),
    CONSTRAINT jobs_attempts_valid CHECK (
        attempts >= 0 AND max_attempts > 0 AND attempts <= max_attempts
    )
);

CREATE INDEX jobs_pending_idx ON jobs (run_at, created_at)
    WHERE state = 'PENDING';
CREATE INDEX jobs_locked_idx ON jobs (locked_at)
    WHERE state = 'RUNNING';
