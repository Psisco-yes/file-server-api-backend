UPDATE users SET storage_quota_bytes = :quota_gb::bigint * 1024 * 1024 * 1024 WHERE username = :'username';
\echo Storage quota for user :username set to :quota_gb GB.