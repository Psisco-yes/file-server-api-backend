param ( [int]$Port = 8085 )
# ==============================================================================
# CONFIG & INIT
# ==============================================================================
$HtmlFile   = Join-Path $PSScriptRoot "index.html"
$ConfigFile = Join-Path $PSScriptRoot "fileserver-config.json"
if (-not (Test-Path $HtmlFile)) {
    Write-Error "Could not find index.html in the script directory."
    exit 1
}
$Global:Config = @{
    Theme         = "light"
    EnvPath       = "../.env"
    DB_Container  = "fileserver_db"
    App_Container = "fileserver_app"
    DB_User       = "fileserver"
    DB_Name       = "fileserver_db"
    DB_Pass       = ""
}
function Import-EnvFile {
    param($Path)
    $envData = @{}
    if (Test-Path $Path) {
        Get-Content $Path | ForEach-Object {
                $line = $_.Trim()
                if ($line -notmatch "^#" -and $line -match "^([^=]+)=(.*)$") {
                    $key = $Matches[1].Trim()
                    $val = $Matches[2].Trim() -replace '^["'']|["'']$', ''
                    $envData[$key] = $val
                }
            }
        }
        return $envData
    }
function Update-Configuration {
    if (Test-Path $ConfigFile) {
        try {
            $json = Get-Content $ConfigFile -Raw | ConvertFrom-Json
            if ($json.Theme) { $Global:Config.Theme = $json.Theme }
            if ($json.DB_Container) { $Global:Config.DB_Container = $json.DB_Container }
            if ($json.App_Container) { $Global:Config.App_Container = $json.App_Container }
            if ($json.EnvPath) { $Global:Config.EnvPath = $json.EnvPath }
        } catch { Write-Warning "Error loading JSON config." }
    }
    $resolvedEnvPath = $Global:Config.EnvPath
    if (-not [System.IO.Path]::IsPathRooted($resolvedEnvPath)) {
        $resolvedEnvPath = Join-Path $PSScriptRoot $resolvedEnvPath
    }
    if (Test-Path $resolvedEnvPath) {
        $envVars = Import-EnvFile $resolvedEnvPath
        if ($envVars["POSTGRES_USER"]) { $Global:Config.DB_User = $envVars["POSTGRES_USER"] }
        if ($envVars["POSTGRES_DB"]) { $Global:Config.DB_Name = $envVars["POSTGRES_DB"] }
        if ($envVars["POSTGRES_PASSWORD"]) { $Global:Config.DB_Pass = $envVars["POSTGRES_PASSWORD"] }
        $Global:Config.EnvFound = $true
    } else {
        $Global:Config.EnvFound = $false
        Write-Warning ".env file not found at: $resolvedEnvPath"
    }
}
function Save-Configuration {
    $toSave = @{
        Theme         = $Global:Config.Theme
        EnvPath       = $Global:Config.EnvPath
        DB_Container  = $Global:Config.DB_Container
        App_Container = $Global:Config.App_Container
    }
    $toSave | ConvertTo-Json | Set-Content $ConfigFile
}
Update-Configuration
# ==============================================================================
# SQL COMMANDS
# ==============================================================================
$SQL = @{
    Stats = @"
    SELECT
    (SELECT COUNT(*) FROM users) AS total_users,
    (SELECT COUNT(*) FROM nodes WHERE node_type = 'file' AND deleted_at IS NULL) AS active_files,
    (SELECT COUNT(*) FROM nodes WHERE node_type = 'folder' AND deleted_at IS NULL) AS active_folders,
    COALESCE(pg_size_pretty((SELECT SUM(size_bytes) FROM nodes WHERE node_type = 'file' AND deleted_at IS NULL)), '0 B') AS total_storage_used,
    (SELECT COUNT(*) FROM shares) AS total_shares,
    (SELECT COUNT(*) FROM sessions WHERE expires_at > NOW()) AS active_sessions
"@
    ListUsers = @"
    SELECT id, username, display_name, pg_size_pretty(storage_quota_bytes) AS quota_pretty, storage_quota_bytes AS quota_raw, pg_size_pretty(storage_used_bytes) AS used_pretty, created_at FROM users ORDER BY id
"@
    AddUser = @"
    CREATE EXTENSION IF NOT EXISTS pgcrypto;
    INSERT INTO users (username, password_hash, display_name, storage_quota_bytes) VALUES (:'username', crypt(:'password', gen_salt('bf')), :'display_name', :quota_bytes)
"@
    ResetPass    = "UPDATE users SET password_hash = crypt(:'new_password', gen_salt('bf')) WHERE username = :'username'"
    ChangeQuota  = "UPDATE users SET storage_quota_bytes = :quota_bytes WHERE username = :'username'"
    KillSessions = "DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE username = :'username')"
    GetFiles     = "SELECT id FROM nodes WHERE owner_id = (SELECT id FROM users WHERE username = :'username') AND node_type = 'file' AND deleted_at IS NULL"
    DeleteUser   = "DELETE FROM users WHERE username = :'username'"
}
# ==============================================================================
# HELPER FUNCTIONS
# ==============================================================================
function Invoke-DbQuery {
    param ([string]$Query, [hashtable]$Params = @{}, [switch]$ReturnObject)
    $psqlArgs = @()
    foreach ($key in $Params.Keys) {
        $psqlArgs += "-v"
        $psqlArgs += "$key=$($Params[$key])"
    }
    $passEnv = "PGPASSWORD='$($Global:Config.DB_Pass)'"
    $ctr = $Global:Config.DB_Container
    $u = $Global:Config.DB_User
    $d = $Global:Config.DB_Name
    if ($ReturnObject) {
        $cmdInput = "COPY ($Query) TO STDOUT WITH CSV HEADER;"
        try {
            $raw = $cmdInput | docker exec -e $passEnv -i $ctr psql -U $u -d $d $psqlArgs 2>&1
            if ($LASTEXITCODE -ne 0) { throw "$raw" }
            return ($raw | ConvertFrom-Csv)
        } catch { return $null }
    } else {
        $output = "$Query;" | docker exec -e $passEnv -i $ctr psql -U $u -d $d $psqlArgs 2>&1
        if ($LASTEXITCODE -ne 0) { throw "$output" }
        return $true
    }
}
function Send-Response {
    param($Context, $StatusCode=200, $Body="", $ContentType="application/json")
    try {
        $Context.Response.StatusCode = $StatusCode
        $Context.Response.ContentType = $ContentType
        $Context.Response.AddHeader("Access-Control-Allow-Origin", "*")
        $buffer = [System.Text.Encoding]::UTF8.GetBytes($Body)
        $Context.Response.ContentLength64 = $buffer.Length
        $Context.Response.OutputStream.Write($buffer, 0, $buffer.Length)
        $Context.Response.Close()
    } catch { }
}
function Convert-QuotaToBytes {
    param($Value, $Unit)
    [int64]$val = [int64]$Value
    if ($Unit -eq "GB") { return $val * 1073741824 }
    if ($Unit -eq "MB") { return $val * 1048576 }
    return $val
}
# ==============================================================================
# HTTP SERVER
# ==============================================================================
$listener = New-Object System.Net.HttpListener
$listener.Prefixes.Add("http://localhost:$Port/")
try {
    $listener.Start()
    Write-Host "=================================================" -ForegroundColor Cyan
    Write-Host " ADMIN PANEL: http://localhost:$Port/" -ForegroundColor Green
    Write-Host " DB User:     $($Global:Config.DB_User)" -ForegroundColor Gray
    Write-Host " Env File:    $($Global:Config.EnvPath)" -ForegroundColor Gray
    Write-Host "================================================="
    $url = "http://localhost:$Port/"
    try {
        if ($IsLinux) { Start-Process "xdg-open" $url }
        elseif ($IsMacOS) { Start-Process "open" $url }
        else { Start-Process $url }
    } catch { Write-Warning "Could not auto-open browser." }
    $keepRunning = $true
    while ($keepRunning -and $listener.IsListening) {
        $contextTask = $listener.GetContextAsync()
        while (-not $contextTask.Wait(500)) {
            if (-not $keepRunning) { break }
        }
        if ($contextTask.Status -ne 'RanToCompletion') { continue }
        $context = $contextTask.Result
        $req     = $context.Request
        $bodyStr = ""
        if ($req.HasEntityBody) {
            $reader = New-Object System.IO.StreamReader($req.InputStream, $req.ContentEncoding)
            $bodyStr = $reader.ReadToEnd()
        }
        $json = if ($bodyStr) { $bodyStr | ConvertFrom-Json } else { $null }
        $route = "$($req.HttpMethod):$($req.Url.AbsolutePath)"
        Write-Host "Request: $route" -ForegroundColor Gray
        try {
            switch ($route) {
                "GET:/" {
                    $html = Get-Content $HtmlFile -Raw -Encoding UTF8
                    Send-Response $context 200 $html "text/html"
                }
                "GET:/api/shutdown" {
                    Send-Response $context 200 '{ "success": true }'
                    Write-Host "Shutdown requested. Exiting..." -ForegroundColor Yellow
                    $keepRunning = $false
                }
                "GET:/api/stats" {
                    $data = Invoke-DbQuery -Query $SQL.Stats -ReturnObject
                    if (!$data) { $data = @{} }
                    Send-Response $context 200 ($data | ConvertTo-Json)
                }
                "GET:/api/users" {
                    $data = Invoke-DbQuery -Query $SQL.ListUsers -ReturnObject
                    if ($null -eq $data) { $data = @() }
                    if ($data -is [PSCustomObject]) { $data = @($data) }
                    Send-Response $context 200 ($data | ConvertTo-Json)
                }
                "POST:/api/user/add" {
                    $qBytes = Convert-QuotaToBytes $json.quota_val $json.quota_unit
                    $p = @{ username=$json.username; password=$json.password; display_name=$json.display_name; quota_bytes=$qBytes }
                    Invoke-DbQuery -Query $SQL.AddUser -Params $p
                    Send-Response $context 200 '{ "success": true }'
                }
                "POST:/api/user/reset-pass" {
                    Invoke-DbQuery -Query $SQL.ResetPass -Params @{ username=$json.username; new_password=$json.new_password }
                    Send-Response $context 200 '{ "success": true }'
                }
                "POST:/api/user/quota" {
                    $qBytes = Convert-QuotaToBytes $json.quota_val $json.quota_unit
                    Invoke-DbQuery -Query $SQL.ChangeQuota -Params @{ username=$json.username; quota_bytes=$qBytes }
                    Send-Response $context 200 '{ "success": true }'
                }
                "POST:/api/user/kill" {
                    Invoke-DbQuery -Query $SQL.KillSessions -Params @{ username=$json.username }
                    Send-Response $context 200 '{ "success": true }'
                }
                "POST:/api/user/delete" {
                    $uName = $json.username
                    $files = Invoke-DbQuery -Query $SQL.GetFiles -Params @{ username=$uName } -ReturnObject
                    if ($files) {
                        if ($files -is [PSCustomObject]) { $files = @($files) }
                        $appContainer = $Global:Config.App_Container
                        foreach ($f in $files) {
                            if ($f.id) {
                                $pathParts = $f.id.ToCharArray() -join "/"
                                docker exec $appContainer rm "/storage/$pathParts" 2>&1 | Out-Null
                            }
                        }
                    }
                    Invoke-DbQuery -Query $SQL.DeleteUser -Params @{ username=$uName }
                    Send-Response $context 200 '{ "success": true }'
                }
                "GET:/api/settings" {
                    $responseConfig = $Global:Config.Clone()
                    $responseConfig.Remove("DB_Pass")
                    Send-Response $context 200 ($responseConfig | ConvertTo-Json)
                }
                "POST:/api/settings" {
                    if ($json.DB_Container) { $Global:Config.DB_Container = $json.DB_Container }
                    if ($json.App_Container) { $Global:Config.App_Container = $json.App_Container }
                    if ($json.EnvPath) { $Global:Config.EnvPath = $json.EnvPath }
                    if ($json.Theme) { $Global:Config.Theme = $json.Theme }
                    Save-Configuration
                    Update-Configuration
                    Send-Response $context 200 '{ "success": true }'
                }
                Default {
                    Send-Response $context 404 '{ "error": "Not Found" }'
                }
            }
        } catch {
            Write-Error $_
            Send-Response $context 500 "{ `"error`": `"$($_.Exception.Message)`" }"
        }
    }
}
finally {
    Write-Host "Shutting down listener..." -ForegroundColor Yellow
    $listener.Stop()
    $listener.Close()
}