[CmdletBinding()]
param(
    [switch]$StartHealthCheck,
    [ValidateRange(1024, 65535)][int]$Port = 18087
)

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)

$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$localRoot = [IO.Path]::GetFullPath((Join-Path $projectRoot '.local'))
$doctorRoot = [IO.Path]::GetFullPath((Join-Path $localRoot 'doctor'))
$localPrefix = $localRoot.TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
if (-not $doctorRoot.StartsWith($localPrefix, [StringComparison]::OrdinalIgnoreCase)) {
    throw "unsafe doctor directory: $doctorRoot"
}

$binaryPath = Join-Path $doctorRoot 'clovergo-doctor.exe'
$runtimeRoot = Join-Path $doctorRoot 'runtime'
$databasePath = Join-Path $runtimeRoot 'doctor.db'
$libraryPath = Join-Path $runtimeRoot 'library'
$stdoutPath = Join-Path $doctorRoot 'server.stdout.log'
$stderrPath = Join-Path $doctorRoot 'server.stderr.log'
$envPath = Join-Path $projectRoot '.env'
$envExamplePath = Join-Path $projectRoot '.env.example'
$process = $null
$savedEnvironment = @{}

function Write-Pass([string]$Message) {
    Write-Host ("[PASS] {0}" -f $Message) -ForegroundColor Green
}

function Write-WarningStatus([string]$Message) {
    Write-Host ("[WARN] {0}" -f $Message) -ForegroundColor Yellow
}

function Read-DotEnv {
    param([Parameter(Mandatory = $true)][string]$Path)

    $values = @{}
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return $values
    }
    foreach ($line in Get-Content -LiteralPath $Path -Encoding UTF8) {
        if ($line -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$') {
            $name = $Matches[1]
            $value = $Matches[2].Trim()
            if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
                $value = $value.Substring(1, $value.Length - 2)
            }
            $values[$name] = $value
        }
    }
    return $values
}

function Get-EffectiveConfig {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [string]$Fallback = ''
    )

    $processValue = [Environment]::GetEnvironmentVariable($Name, 'Process')
    if (-not [string]::IsNullOrWhiteSpace($processValue)) {
        return $processValue.Trim()
    }
    if ($script:localConfig.ContainsKey($Name) -and -not [string]::IsNullOrWhiteSpace([string]$script:localConfig[$Name])) {
        return ([string]$script:localConfig[$Name]).Trim()
    }
    return $Fallback
}

function Save-Environment([string[]]$Names) {
    foreach ($name in $Names) {
        $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
    }
}

function Restore-Environment {
    foreach ($entry in $savedEnvironment.GetEnumerator()) {
        [Environment]::SetEnvironmentVariable($entry.Key, $entry.Value, 'Process')
    }
}

if (-not (Test-Path -LiteralPath $projectRoot -PathType Container)) {
    throw "project directory is missing: $projectRoot"
}
Write-Pass "project directory exists: $projectRoot"

foreach ($requiredFile in @('go.mod', 'main.go', '.env.example')) {
    $path = Join-Path $projectRoot $requiredFile
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "required project file is missing: $path"
    }
}
Write-Pass 'required project files exist'

foreach ($directory in @((Join-Path $projectRoot 'data'), (Join-Path $projectRoot 'data\library'))) {
    if (Test-Path -LiteralPath $directory -PathType Container) {
        Write-Pass "directory exists: $directory"
    } else {
        Write-WarningStatus "directory does not exist yet and will be created on install/start: $directory"
    }
}

$go = Get-Command go.exe -ErrorAction Stop
$goVersion = (& $go.Source version)
if ($LASTEXITCODE -ne 0) {
    throw 'go version failed'
}
Write-Pass $goVersion

$exampleConfig = Read-DotEnv -Path $envExamplePath
$expectedNames = @(
    'APP_ADDR', 'DATABASE_PATH', 'LIBRARY_DIR', 'MAX_UPLOAD_MB', 'MAX_DOCUMENT_RUNES',
    'ARK_BASE_URL', 'ARK_API_KEY', 'ARK_MODEL', 'ARK_TIMEOUT_SECONDS', 'AUTO_PUBLISH_MIN_CONFIDENCE'
)
$missingNames = @($expectedNames | Where-Object { -not $exampleConfig.ContainsKey($_) })
if ($missingNames.Count -gt 0) {
    throw "configuration template is missing names: $($missingNames -join ', ')"
}
$legacyNames = @($exampleConfig.Keys | Where-Object { $_ -like 'LLM_*' })
if ($legacyNames.Count -gt 0) {
    throw "configuration template still contains legacy names: $($legacyNames -join ', ')"
}
Write-Pass 'configuration template contains the expected APP_*, data, and ARK_* names'

$script:localConfig = Read-DotEnv -Path $envPath
if (Test-Path -LiteralPath $envPath -PathType Leaf) {
    Write-Pass 'local .env file exists'
} else {
    Write-WarningStatus 'local .env is absent; copy .env.example or run install.ps1 before AI use'
}
$localLegacyNames = @($script:localConfig.Keys | Where-Object { $_ -like 'LLM_*' })
if ($localLegacyNames.Count -gt 0) {
    Write-WarningStatus "local .env contains ignored legacy LLM_* names: $($localLegacyNames -join ', ')"
}

$appAddress = Get-EffectiveConfig -Name 'APP_ADDR' -Fallback '127.0.0.1:8088'
if ($appAddress -notmatch '^(\[[0-9A-Fa-f:]+\]|[A-Za-z0-9.-]+):(\d{1,5})$') {
    throw 'APP_ADDR must use host:port format'
}
$appPort = [int]$Matches[2]
if ($appPort -lt 1 -or $appPort -gt 65535) {
    throw 'APP_ADDR port must be between 1 and 65535'
}
Write-Pass 'APP_ADDR format is valid (value hidden)'
$appHost = $Matches[1].ToLowerInvariant()
if ($appHost -ne 'localhost') {
    $parsedAppIP = $null
    if (-not [Net.IPAddress]::TryParse($appHost.Trim('[', ']'), [ref]$parsedAppIP) -or -not [Net.IPAddress]::IsLoopback($parsedAppIP)) {
        throw 'APP_ADDR must use a loopback address; LAN/public bindings are not supported'
    }
}

$arkBaseURL = Get-EffectiveConfig -Name 'ARK_BASE_URL' -Fallback 'https://ark.cn-beijing.volces.com/api/v3'
$arkModel = Get-EffectiveConfig -Name 'ARK_MODEL' -Fallback 'deepseek-v4-1-flash-260910'
$arkAPIKey = Get-EffectiveConfig -Name 'ARK_API_KEY'
$arkUri = $null
$arkURLValid = [Uri]::TryCreate($arkBaseURL, [UriKind]::Absolute, [ref]$arkUri) -and $arkUri.Scheme -eq 'https' -and ($arkUri.Host -eq 'volces.com' -or $arkUri.Host.EndsWith('.volces.com', [StringComparison]::OrdinalIgnoreCase))
if ($arkURLValid) {
    Write-Pass 'ARK_BASE_URL is present and uses an official HTTPS host'
} else {
    Write-WarningStatus 'ARK_BASE_URL is missing or invalid'
}
if ([string]::IsNullOrWhiteSpace($arkModel)) {
    Write-WarningStatus 'ARK_MODEL is not configured'
} else {
    Write-Pass 'ARK_MODEL is configured (value hidden)'
}
if ([string]::IsNullOrWhiteSpace($arkAPIKey)) {
    Write-WarningStatus 'ARK_API_KEY is not configured; AI endpoints will return ARK_NOT_CONFIGURED'
} else {
    Write-Pass 'ARK_API_KEY is configured (value hidden)'
}

New-Item -ItemType Directory -Path $doctorRoot -Force | Out-Null
Push-Location -LiteralPath $projectRoot
try {
    & $go.Source build -o $binaryPath .
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}
Write-Pass "go build completed: $binaryPath"

if ($StartHealthCheck) {
    # The health check always starts from an empty scratch database so a file
    # left by an older CloverGo version cannot block it.
    if (Test-Path -LiteralPath $runtimeRoot) {
        $runtimeFull = [IO.Path]::GetFullPath($runtimeRoot)
        if (-not $runtimeFull.StartsWith($localPrefix, [StringComparison]::OrdinalIgnoreCase)) {
            throw "refusing to remove an unverified doctor runtime directory: $runtimeFull"
        }
        Remove-Item -LiteralPath $runtimeFull -Recurse -Force
    }
    New-Item -ItemType Directory -Path $libraryPath -Force | Out-Null
    Save-Environment @(
        'APP_ADDR', 'DATABASE_PATH', 'LIBRARY_DIR', 'ARK_BASE_URL', 'ARK_API_KEY', 'ARK_MODEL', 'ARK_TIMEOUT_SECONDS'
    )
    try {
        [Environment]::SetEnvironmentVariable('APP_ADDR', "127.0.0.1:$Port", 'Process')
        [Environment]::SetEnvironmentVariable('DATABASE_PATH', $databasePath, 'Process')
        [Environment]::SetEnvironmentVariable('LIBRARY_DIR', $libraryPath, 'Process')
        [Environment]::SetEnvironmentVariable('ARK_BASE_URL', 'https://ark.cn-beijing.volces.com/api/v3', 'Process')
        [Environment]::SetEnvironmentVariable('ARK_API_KEY', ' ', 'Process')
        [Environment]::SetEnvironmentVariable('ARK_MODEL', 'doctor-no-key', 'Process')
        [Environment]::SetEnvironmentVariable('ARK_TIMEOUT_SECONDS', '5', 'Process')

        $process = Start-Process -FilePath $binaryPath -WorkingDirectory $projectRoot -PassThru -WindowStyle Hidden `
            -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
        $health = $null
        for ($attempt = 1; $attempt -le 40; $attempt++) {
            if ($process.HasExited) {
                throw "health-check process exited early; inspect $stderrPath"
            }
            try {
                $health = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/health/ready" -Method Get -TimeoutSec 2
                break
            } catch {
                Start-Sleep -Milliseconds 250
            }
        }
        if ($null -eq $health) {
            throw "health endpoint did not become ready; inspect $stderrPath"
        }
        if ($health.data.status -ne 'ready' -or $health.data.ark_configured -ne $false) {
            throw 'health endpoint returned an unexpected readiness or Ark configuration state'
        }
        Write-Pass 'optional health check passed with Ark disabled; no paid AI endpoint was called'
    } finally {
        if ($null -ne $process -and -not $process.HasExited) {
            Stop-Process -Id $process.Id -Force
            $process.WaitForExit()
        }
        Restore-Environment
    }
}

Write-Host ''
Write-Host 'CloverGo doctor completed.' -ForegroundColor Cyan
