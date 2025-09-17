# Terminates all active sessions for a user (logs them out everywhere).
# USAGE: .\scripts\terminate-sessions.ps1 -Username "user"

param (
    [Parameter(Mandatory=$true)]
    [string]$Username
)

Write-Host "Terminating all sessions for user '$Username'..."

Get-Content -Path ".\scripts\sql\terminatesessions.sql" -Raw | docker exec -i fileserver_db psql -U fileserver -d fileserver_db `
    -v username="'$Username'"