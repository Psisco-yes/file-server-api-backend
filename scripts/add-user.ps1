# Adds a new user to the system.
# USAGE: .\scripts\add-user.ps1 -Username "newuser" -Password "P@ssword123" -DisplayName "New User Name"
[System.Diagnostics.CodeAnalysis.SuppressMessageAttribute("PSAvoidUsingPlainTextForPassword", "")]
param (
    [Parameter(Mandatory=$true)]
    [string]$Username,
    
    [Parameter(Mandatory=$true)]
    [string]$Password,
    
    [string]$DisplayName = $Username
)

Write-Host "Creating user '$Username'..."

Get-Content -Path ".\scripts\sql\adduser.sql" -Raw | docker exec -i fileserver_db psql -U fileserver -d fileserver_db `
    -v username="'$Username'" `
    -v password="'$Password'" `
    -v display_name="'$DisplayName'"