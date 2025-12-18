#!/bin/sh

export PGUSER=${POSTGRES_USER}
export PGPASSWORD=${POSTGRES_PASSWORD}
export PGHOST=db

echo "Running session cleanup..."
psql -d ${POSTGRES_DB} -c "DELETE FROM sessions WHERE expires_at < NOW();"
echo "Session cleanup finished."