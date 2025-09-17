SELECT 
    (SELECT COUNT(*) FROM users) AS total_users,
    (SELECT COUNT(*) FROM nodes WHERE node_type = 'file' AND deleted_at IS NULL) AS active_files,
    (SELECT COUNT(*) FROM nodes WHERE node_type = 'folder' AND deleted_at IS NULL) AS active_folders,
    COALESCE(pg_size_pretty((SELECT SUM(size_bytes) FROM nodes WHERE node_type = 'file' AND deleted_at IS NULL)), '0 B') AS total_storage_used,
    (SELECT COUNT(*) FROM shares) AS total_shares,
    (SELECT COUNT(*) FROM sessions WHERE expires_at > NOW()) AS active_sessions;