# Displays overall statistics for the file server.
# USAGE: .\scripts\system-stats.ps1

Write-Host "Fetching system statistics..."

Get-Content -Path ".\scripts\sql\systemstats.sql" -Raw | docker exec -i fileserver_db psql -x -U fileserver -d fileserver_db