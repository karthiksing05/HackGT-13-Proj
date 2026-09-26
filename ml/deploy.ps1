<#
.SYNOPSIS
    Deploys the contents of the ml folder to /opt/ml on the VPS,
    sets chmod 755, installs ml.service, and restarts the ml systemd service.
#>
[CmdletBinding()]
param (
    [string]$ServerHost,
    [string]$ServerUser,
    [string]$ServerPassword,
    [string]$RemoteDir = "/opt/ml",
    [string]$ServiceName = "ml"
)

$ErrorActionPreference = "Stop"

# Load environment configuration from .env if present
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $scriptDir) { $scriptDir = (Get-Location).Path }

$envCandidates = @(
    Join-Path $scriptDir ".env",
    Join-Path $scriptDir "..\Backend\.env",
    Join-Path $scriptDir "..\.env"
)

foreach ($envFile in $envCandidates) {
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
        break
    }
}

# Resolve settings from parameters -> .env variables -> fallback defaults
if (-not $ServerHost) { $ServerHost = if ($env:DEPLOY_HOST) { $env:DEPLOY_HOST } else { "45.32.223.40" } }
if (-not $ServerUser) { $ServerUser = if ($env:DEPLOY_USER) { $env:DEPLOY_USER } else { "root" } }
if (-not $ServerPassword) { $ServerPassword = if ($env:DEPLOY_PASSWORD) { $env:DEPLOY_PASSWORD } else { "" } }
if (-not $RemoteDir) { $RemoteDir = if ($env:ML_DEPLOY_REMOTE_DIR) { $env:ML_DEPLOY_REMOTE_DIR } else { "/opt/ml" } }

$targetHost = "${ServerUser}@${ServerHost}"

Write-Host "============================================================" -ForegroundColor Cyan
Write-Host "  SideQuestz ML Deploy: ${targetHost}:${RemoteDir}" -ForegroundColor Cyan
Write-Host "============================================================" -ForegroundColor Cyan

# 1. Prepare remote directory
Write-Host "==> [1/3] Preparing remote directory $RemoteDir..." -ForegroundColor Cyan
& ssh -o StrictHostKeyChecking=accept-new $targetHost "mkdir -p $RemoteDir"

# 2. Upload ML files via tar or scp
Write-Host "==> [2/3] Uploading ML files to ${targetHost}:${RemoteDir}..." -ForegroundColor Cyan
if ($ServerPassword) {
    Write-Host "    Server password (from .env): $ServerPassword" -ForegroundColor Yellow
}

if (Get-Command bash -ErrorAction SilentlyContinue) {
    bash ./deploy.sh $ServerHost $ServerUser $ServerPassword
} else {
    # Direct scp fallback
    & scp -o StrictHostKeyChecking=accept-new -r (Get-ChildItem -Path $scriptDir -Exclude @("__pycache__", ".pytest_cache", ".venv", "venv", ".git", "runs") | ForEach-Object { $_.FullName }) "${targetHost}:${RemoteDir}/"

    # 3. chmod 755 and systemctl restart ml
    Write-Host "==> [3/3] Setting chmod 755, setting up environment, and restarting $ServiceName..." -ForegroundColor Cyan
    $remoteCmd = "chmod -R 755 $RemoteDir; if [ ! -d '$RemoteDir/.venv' ]; then python3 -m venv $RemoteDir/.venv; $RemoteDir/.venv/bin/pip install --upgrade pip; fi; if [ -f '$RemoteDir/requirements.txt' ]; then $RemoteDir/.venv/bin/pip install -q -r $RemoteDir/requirements.txt; fi; if [ -f '$RemoteDir/ml.service' ]; then cp '$RemoteDir/ml.service' /etc/systemd/system/ml.service; systemctl daemon-reload; systemctl enable ml; fi; systemctl restart $ServiceName; sleep 1; systemctl status $ServiceName --no-pager"
    & ssh -o StrictHostKeyChecking=accept-new $targetHost $remoteCmd
}

Write-Host ""
Write-Host "============================================================" -ForegroundColor Green
Write-Host "  ✓ Deployment complete! $ServiceName restarted on $ServerHost. 🚀" -ForegroundColor Green
Write-Host "============================================================" -ForegroundColor Green
