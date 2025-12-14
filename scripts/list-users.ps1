# Lists all users in the system with their storage details.
# USAGE: .\list-users.ps1

Write-Host "Fetching user list from the database..."

Get-Content -Path ".\sql\listusers.sql" -Raw | docker exec -i fileserver_db psql -x -U fileserver -d fileserver_db