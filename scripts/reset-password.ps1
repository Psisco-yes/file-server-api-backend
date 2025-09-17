# Resets a user's password.
# USAGE: .\scripts\reset-password.ps1 -Username "user" -NewPassword "SomeSecureP@ss!"
[System.Diagnostics.CodeAnalysis.SuppressMessageAttribute("PSAvoidUsingPlainTextForPassword", "")]
param (
    [Parameter(Mandatory=$true)]
    [string]$Username,
    
    [Parameter(Mandatory=$true)]
    [string]$NewPassword
)

Write-Host "Resetting password for user '$Username'..."

Get-Content -Path ".\scripts\sql\resetpassword.sql" -Raw | docker exec -i fileserver_db psql -U fileserver -d fileserver_db `
    -v username="'$Username'" `
    -v new_password="'$NewPassword'"