<#
.SYNOPSIS
    Builds the production binary for SideQuestz Backend targeting a Linux VPS or local environment.
.EXAMPLE
    .\build.ps1
    .\build.ps1 -TargetArch arm64
    .\build.ps1 -SkipTests
    .\build.ps1 -TargetOS windows
#>
[CmdletBinding()]
param (
    [string]$TargetOS = "linux",
    [string]$TargetArch = "amd64",
    [switch]$SkipTests,
    [switch]$Clean
)

$ErrorActionPreference = "Stop"

Write-Host "==> Starting production build for SideQuestz Backend..." -ForegroundColor Cyan

$origGOOS = $env:GOOS
$origGOARCH = $env:GOARCH
$origCGO = $env:CGO_ENABLED

try {
    if ($Clean) {
        Write-Host "==> Cleaning previous artifacts..." -ForegroundColor Yellow
        if (Test-Path "bin") {
            Remove-Item "bin" -Recurse -Force
        }
    }

    if (-not (Test-Path "bin")) {
        New-Item -ItemType Directory -Path "bin" | Out-Null
    }

    Write-Host "  Target OS:   $TargetOS" -ForegroundColor Cyan
    Write-Host "  Target Arch: $TargetArch" -ForegroundColor Cyan
    Write-Host "  Go Version:  $(go version)" -ForegroundColor Cyan

    Write-Host "==> Downloading Go modules..." -ForegroundColor Cyan
    go mod download

    if (-not $SkipTests) {
        Write-Host "==> Running test suite..." -ForegroundColor Cyan
        go test -v ./...
        if ($LASTEXITCODE -ne 0) {
            Write-Error "Tests failed! Aborting build."
            exit $LASTEXITCODE
        }
        Write-Host "==> Tests passed successfully!" -ForegroundColor Green
    } else {
        Write-Host "==> Skipping tests (-SkipTests specified)" -ForegroundColor Yellow
    }

    $binaryName = "sidequestz-server"
    if ($TargetOS -eq "windows") {
        $binaryName += ".exe"
    }
    $outputPath = "bin/$binaryName"

    Write-Host "==> Compiling static binary ($outputPath)..." -ForegroundColor Cyan

    $env:CGO_ENABLED = "0"
    $env:GOOS = $TargetOS
    $env:GOARCH = $TargetArch

    go build -trimpath -ldflags="-s -w" -o $outputPath main.go

    if ($LASTEXITCODE -ne 0) {
        Write-Error "Build failed!"
        exit $LASTEXITCODE
    }

    $item = Get-Item $outputPath
    $mbSize = [math]::Round($item.Length / 1MB, 2)

    Write-Host "==> Build successful!" -ForegroundColor Green
    Write-Host "    Binary: $outputPath ($mbSize MB)" -ForegroundColor Green
    Write-Host ""
    Write-Host "To deploy to your VPS:" -ForegroundColor White
    Write-Host "  scp $outputPath user@your-vps-ip:/opt/sidequestz/" -ForegroundColor Yellow
    Write-Host ""
}
finally {
    $env:GOOS = $origGOOS
    $env:GOARCH = $origGOARCH
    $env:CGO_ENABLED = $origCGO
}
