SELECT 
    id, 
    username, 
    display_name, 
    pg_size_pretty(storage_quota_bytes) AS quota,
    pg_size_pretty(storage_used_bytes) AS used,
    created_at
FROM users
ORDER BY id;