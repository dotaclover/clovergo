[CmdletBinding()]
param(
    [string]$InstallDir,
    # A prebuilt clovergo.exe (for example from scripts/build.ps1). When set,
    # the target machine does not need the Go toolchain.
    [string]$Binary
)

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)

$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$localRoot = [IO.Path]::GetFullPath((Join-Path $projectRoot '.local'))
$installWasExplicit = $PSBoundParameters.ContainsKey('InstallDir') -and -not [string]::IsNullOrWhiteSpace($InstallDir)
if (-not $installWasExplicit) {
    $InstallDir = Join-Path $localRoot 'install'
}
$installRoot = [IO.Path]::GetFullPath($InstallDir)

function Test-IsChildPath {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Parent
    )

    $prefix = $Parent.TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    return $Path.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)
}

function Assert-NoReparsePoints([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        return
    }
    $items = @(Get-ChildItem -LiteralPath $Path -Force -Recurse -ErrorAction Stop)
    foreach ($item in $items) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "refusing to use an install directory containing a reparse point: $($item.FullName)"
        }
    }
}

$isProjectLocal = Test-IsChildPath -Path $installRoot -Parent $localRoot
if (-not $isProjectLocal -and -not $installWasExplicit) {
    throw "default install path must remain inside the project .local directory: $installRoot"
}

$volumeRoot = [IO.Path]::GetPathRoot($installRoot)
$normalizedTarget = $installRoot.TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar)
$normalizedVolumeRoot = $volumeRoot.TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar)
if ([string]::Equals($normalizedTarget, $normalizedVolumeRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw "refusing to install into a filesystem root: $installRoot"
}
if ([string]::Equals($installRoot, $projectRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw "refusing to install over the project source directory: $installRoot"
}
if ((Test-Path -LiteralPath $installRoot) -and -not (Test-Path -LiteralPath $installRoot -PathType Container)) {
    throw "install target exists but is not a directory: $installRoot"
}
if (Test-Path -LiteralPath $installRoot) {
    $existingInstall = Get-Item -LiteralPath $installRoot -Force
    if (($existingInstall.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "refusing to install through a reparse point: $installRoot"
    }
    Assert-NoReparsePoints $installRoot
}

$binaryPath = Join-Path $installRoot 'clovergo.exe'
$dataPath = Join-Path $installRoot 'data'
$libraryPath = Join-Path $dataPath 'library'
$envPath = Join-Path $installRoot '.env'
$envExamplePath = Join-Path $projectRoot '.env.example'

if (-not (Test-Path -LiteralPath $envExamplePath -PathType Leaf)) {
    throw "missing project configuration template: $envExamplePath"
}

New-Item -ItemType Directory -Path $installRoot -Force | Out-Null
New-Item -ItemType Directory -Path $libraryPath -Force | Out-Null

if (-not [string]::IsNullOrWhiteSpace($Binary)) {
    $sourceBinary = (Resolve-Path -LiteralPath $Binary -ErrorAction Stop).Path
    Copy-Item -LiteralPath $sourceBinary -Destination $binaryPath -Force
} else {
    & (Join-Path $PSScriptRoot 'build.ps1') -Output $binaryPath
    if ($LASTEXITCODE -ne 0) {
        throw "build.ps1 failed with exit code $LASTEXITCODE"
    }
}

$createdEnv = $false
if (-not (Test-Path -LiteralPath $envPath)) {
    Copy-Item -LiteralPath $envExamplePath -Destination $envPath
    $createdEnv = $true
}

Write-Host '[PASS] CloverGo executable installed.' -ForegroundColor Green
Write-Host ("       Binary: {0}" -f $binaryPath)
Write-Host ("       Library: {0}" -f $libraryPath)
if ($createdEnv) {
    Write-Host ("[PASS] Created configuration file: {0}" -f $envPath) -ForegroundColor Green
} else {
    Write-Host ("[PASS] Kept existing configuration file: {0}" -f $envPath) -ForegroundColor Green
}
Write-Host ''
Write-Host 'Before AI ingestion or content generation, set ARK_API_KEY in the local .env file.' -ForegroundColor Yellow
Write-Host 'No API key was written and no Windows service was registered.'
Write-Host 'The executable reads .env, data\clovergo.db and data\library next to itself, whatever the working directory.'
Write-Host ("Start manually: Set-Location '{0}'; .\clovergo.exe" -f $installRoot)
