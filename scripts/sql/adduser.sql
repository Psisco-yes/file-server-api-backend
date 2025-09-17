CREATE EXTENSION IF NOT EXISTS pgcrypto;

INSERT INTO users (username, password_hash, display_name)
VALUES (:'username', crypt(:'password', gen_salt('bf')), :'display_name');

\echo User :username created successfully.