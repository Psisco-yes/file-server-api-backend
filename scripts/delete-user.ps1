# Deletes a user and all of their physical files from storage.
# USAGE: .\scripts\delete-user.ps1 -Username "user_to_delete"

param (
    [Parameter(Mandatory=$true)]
    [string]$Username
)

Write-Host "Starting deletion process for user: $Username" -ForegroundColor Yellow

Write-Host "Fetching list of files owned by the user..."
$fileIdsContent = Get-Content -Path ".\scripts\sql\getfilesforuser.sql" -Raw | docker exec -i fileserver_db psql -U fileserver -d fileserver_db `
    -v username="$Username" | Out-String

if ($LASTEXITCODE -ne 0) {
    Write-Host "Error fetching file list from database. Aborting." -ForegroundColor Red
    exit 1
}

$lines = $fileIdsContent.Split([Environment]::NewLine, [StringSplitOptions]::RemoveEmptyEntries)

if ($lines.Count -lt 3) {
    $fileIdArray = @()
} else {
    $fileIdArray = $lines | Select-Object -Skip 2 | Select-Object -SkipLast 1 | ForEach-Object { $_.Trim() }
}

Write-Host "Found $($fileIdArray.Count) files to delete from storage."

$errorsOccurred = $false
if ($fileIdArray.Count -gt 0) {
    Write-Host "Deleting physical files from storage volume..."
    foreach ($fileId in $fileIdArray) {
        if (-not [string]::IsNullOrWhiteSpace($fileId)) {
            $filePathInContainer = "/storage/" + ($fileId.ToCharArray() -join "/")
            
            Write-Host "  - Deleting file: $filePathInContainer"
            
            docker exec fileserver_app rm $filePathInContainer
            
            if ($LASTEXITCODE -ne 0) {
                Write-Host "  - FAILED to delete file: $filePathInContainer. Aborting before database deletion." -ForegroundColor Red
                $errorsOccurred = $true
                break
            }
        }
    }
    
    if ($errorsOccurred) {
        Write-Host "Errors occurred during physical file deletion. The user has NOT been deleted from the database. Please resolve the file system issues and run the script again." -ForegroundColor Red
        exit 1
    }

    Write-Host "Physical file cleanup complete." -ForegroundColor Green
} else {
    Write-Host "User had no physical files to delete."
}

Write-Host "Deleting user '$Username' from the database..."
Get-Content -Path ".\scripts\sql\deleteuser.sql" -Raw | docker exec -i fileserver_db psql -U fileserver -d fileserver_db `
    -v username="$Username" | Out-String

if ($LASTEXITCODE -ne 0) {
    Write-Host "CRITICAL: Physical files were deleted, but there was an error deleting the user from the database. Please investigate manually." -ForegroundColor Red
    exit 1
}

Write-Host "Database records for user '$Username' have been deleted." -ForegroundColor Green
Write-Host "Successfully purged user '$Username' and all associated data." -ForegroundColor Cyan