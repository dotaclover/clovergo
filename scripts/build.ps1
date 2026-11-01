[CmdletBinding()]
param(
    [string]$Output,
    # Rebuild web/dist from frontend/ with npm before compiling. web/dist is
    # committed, so this is only needed after frontend changes.
    [switch]$Frontend
)

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)

$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
if ([string]::IsNullOrWhiteSpace($Output)) {
    $Output = Join-Path $projectRoot '.local\dist\clovergo.exe'
}
$outputPath = [IO.Path]::GetFullPath($Output)
New-Item -ItemType Directory -Path (Split-Path -Parent $outputPath) -Force | Out-Null

if ($Frontend) {
    $npm = Get-Command npm.cmd -ErrorAction Stop
    Push-Location -LiteralPath (Join-Path $projectRoot 'frontend')
    try {
        & $npm.Source ci
        if ($LASTEXITCODE -ne 0) { throw "npm ci failed with exit code $LASTEXITCODE" }
        & $npm.Source run build
        if ($LASTEXITCODE -ne 0) { throw "npm run build failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
}

$go = Get-Command go.exe -ErrorAction Stop
$version = (Get-Content -Raw -Encoding UTF8 -LiteralPath (Join-Path $projectRoot 'VERSION')).Trim()
if ($version -notmatch '^\d+\.\d+\.\d+([+-][0-9A-Za-z.-]+)?$') {
    throw 'VERSION must contain a semantic version'
}

Push-Location -LiteralPath $projectRoot
try {
    & $go.Source build -trimpath -ldflags "-s -w -X main.version=$version" -o $outputPath .
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}
Write-Host ("[PASS] Built CloverGo {0}: {1}" -f $version, $outputPath) -ForegroundColor Green
