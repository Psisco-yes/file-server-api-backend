CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    storage_quota_bytes BIGINT NOT NULL DEFAULT 5368709120,
    storage_used_bytes BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE sessions (
    id UUID PRIMARY KEY, 
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token TEXT UNIQUE NOT NULL,
    user_agent TEXT,
    client_ip TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);

CREATE TABLE nodes (
    id VARCHAR(21) PRIMARY KEY,
    owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id VARCHAR(21) REFERENCES nodes(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    node_type VARCHAR(10) NOT NULL CHECK (node_type IN ('file', 'folder')),
    size_bytes BIGINT,
    mime_type VARCHAR(255),
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    modified_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    deleted_at TIMESTAMPTZ,
    original_parent_id VARCHAR(21),
    
    CONSTRAINT unique_name_in_parent UNIQUE (owner_id, parent_id, name)
);

CREATE UNIQUE INDEX unique_name_in_folder ON nodes (owner_id, parent_id, name) WHERE parent_id IS NOT NULL;
CREATE UNIQUE INDEX unique_name_in_root ON nodes (owner_id, name) WHERE parent_id IS NULL;

CREATE INDEX idx_nodes_owner_id ON nodes(owner_id);
CREATE INDEX idx_nodes_parent_id ON nodes(parent_id);

CREATE TABLE shares (
    id SERIAL PRIMARY KEY,
    node_id VARCHAR(21) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    sharer_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permissions VARCHAR(20) NOT NULL CHECK (permissions IN ('read', 'write')),
    shared_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,

    CONSTRAINT unique_share_per_recipient UNIQUE (node_id, recipient_id)
);

CREATE TABLE user_favorites (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id VARCHAR(21) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, node_id)
);

CREATE TABLE event_journal (
    id BIGSERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL,
    event_time TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    payload JSONB NOT NULL
);

CREATE INDEX idx_event_journal_user_id_id ON event_journal(user_id, id);

INSERT INTO users (username, password_hash, display_name, storage_quota_bytes)
VALUES ('admin', '$2a$12$Q5YPzisDD241y55p0fwlJe/myrAlTl4BEzromC5nKzDM6jK33XaBK', 'Administrator', 10485760);

INSERT INTO users (username, password_hash, display_name, storage_quota_bytes)
VALUES ('user', '$2a$12$YVeabseYD5moPjzMWjtMQOgc4sx0U4avHCOW5AdfLm41TTHEYrWlC', 'Test User', 10485760);