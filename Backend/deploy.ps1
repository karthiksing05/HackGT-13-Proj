<#
.SYNOPSIS
    Builds the Linux binary via build.sh, copies it via SSH/SCP into /opt/backend
    using credentials loaded from .env, sets chmod 755, and restarts the sidequestz systemd service.
#>
[CmdletBinding()]
param (
    [string]$ServerHost,
    [string]$ServerUser,
    [string]$ServerPassword,
    [string]$ServiceName,
    [string]$RemoteDir,
    [string]$BinaryName = "sidequestz-server"
)

$ErrorActionPreference = "Stop"

# Load environment configuration from .env if present
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $scriptDir) { $scriptDir = (Get-Location).Path }
$envFile = Join-Path $scriptDir ".env"

if (Test-Path $envFile) {
    Get-Content $envFile | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith("#") -and $line.Contains("=")) {
            $parts = $line -split "=", 2
            $key = $parts[0].Trim()
            $val = $parts[1].Trim()
            if ($val.StartsWith("'") -and $val.EndsWith("'")) {
                $val = $val.Substring(1, $val.Length - 2)
            } elseif ($val.StartsWith("`"") -and $val.EndsWith("`"")) {
                $val = $val.Substring(1, $val.Length - 2)
            }
            [System.Environment]::SetEnvironmentVariable($key, $val, "Process")
        }
    }
}

# Resolve settings from parameters -> .env variables -> fallback defaults
if (-not $ServerHost) { $ServerHost = if ($env:DEPLOY_HOST) { $env:DEPLOY_HOST } else { "45.32.223.40" } }
if (-not $ServerUser) { $ServerUser = if ($env:DEPLOY_USER) { $env:DEPLOY_USER } else { "root" } }
if (-not $ServerPassword) { $ServerPassword = if ($env:DEPLOY_PASSWORD) { $env:DEPLOY_PASSWORD } else { "" } }
if (-not $RemoteDir) { $RemoteDir = if ($env:DEPLOY_REMOTE_DIR) { $env:DEPLOY_REMOTE_DIR } else { "/opt/backend" } }
if (-not $ServiceName) { $ServiceName = if ($env:DEPLOY_SERVICE) { $env:DEPLOY_SERVICE } else { "sidequestz" } }

$targetHost = "${ServerUser}@${ServerHost}"
$targetDest = "${targetHost}:${RemoteDir}/${BinaryName}"

Write-Host "============================================================" -ForegroundColor Cyan
Write-Host "  SideQuestz Deploy: $targetDest" -ForegroundColor Cyan
Write-Host "============================================================" -ForegroundColor Cyan

# 1. Run build
Write-Host "==> [1/3] Building Linux amd64 binary..." -ForegroundColor Cyan
if (Get-Command bash -ErrorAction SilentlyContinue) {
    bash ./build.sh --amd64
} else {
    $env:CGO_ENABLED = "0"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    if (-not (Test-Path "bin")) { New-Item -ItemType Directory -Path "bin" | Out-Null }
    go build -trimpath -ldflags="-s -w" -o "bin/$BinaryName" main.go
}

if (-not (Test-Path "bin/$BinaryName")) {
    Write-Error "Binary bin/$BinaryName was not found. Build failed."
    exit 1
}
Write-Host "    ✓ Build succeeded: bin/$BinaryName" -ForegroundColor Green

# 2. Upload to server via SSH into /opt/backend
Write-Host "==> [2/3] Copying binary via SSH to $targetDest..." -ForegroundColor Cyan
if ($ServerPassword) {
    Write-Host "    Server password (from .env): $ServerPassword" -ForegroundColor Yellow
}

# Ensure remote directory exists
& ssh -o StrictHostKeyChecking=accept-new $targetHost "mkdir -p $RemoteDir"

# SCP binary to server
& scp -o StrictHostKeyChecking=accept-new "bin/$BinaryName" $targetDest

# 3. chmod 755 and systemctl restart sidequestz
Write-Host "==> [3/3] Setting chmod 755 and restarting $ServiceName..." -ForegroundColor Cyan
$remoteCmd = "chmod 755 $RemoteDir/$BinaryName; systemctl restart $ServiceName; sleep 1; systemctl status $ServiceName --no-pager"
& ssh -o StrictHostKeyChecking=accept-new $targetHost $remoteCmd

Write-Host ""
Write-Host "============================================================" -ForegroundColor Green
Write-Host "  ✓ Deployment complete! $ServiceName restarted on $ServerHost. 🚀" -ForegroundColor Green
Write-Host "============================================================" -ForegroundColor Green
