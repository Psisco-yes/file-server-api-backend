DELETE FROM sessions
WHERE user_id = (SELECT id FROM users WHERE username = :'username');

\echo All active sessions for user :username have been terminated.