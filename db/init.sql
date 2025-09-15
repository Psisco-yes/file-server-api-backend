CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    storage_quota_bytes BIGINT NOT NULL DEFAULT 5368709120,
    storage_used_bytes BIGINT NOT NULL DEFAULT 0
);

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