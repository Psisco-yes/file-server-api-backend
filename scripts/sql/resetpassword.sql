UPDATE users
SET password_hash = crypt(:'new_password', gen_salt('bf'))
WHERE username = :'username';

\echo Password for user :username has been reset.