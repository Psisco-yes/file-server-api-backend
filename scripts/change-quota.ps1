# Changes the storage quota for a specific user.
# USAGE: .\change-quota.ps1 -Username "user" -QuotaGB 50

param (
    [Parameter(Mandatory=$true)]
    [string]$Username,
    
    [Parameter(Mandatory=$true)]
    [int]$QuotaGB
)

Write-Host "Setting storage quota for user '$Username' to $QuotaGB GB..."

Get-Content -Path ".\sql\changequota.sql" -Raw | docker exec -i fileserver_db psql -U fileserver -d fileserver_db `
    -v username="$Username" `
    -v quota_gb=$QuotaGB