$ErrorActionPreference = 'Stop'
$script:WalletRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))

function Invoke-WalletDocker {
    param([Parameter(Mandatory=$true)][string[]]$DockerArguments)
    $previous = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        & docker @DockerArguments 2>&1 | ForEach-Object { $_.ToString() }
        $dockerExit = $LASTEXITCODE
    } finally { $ErrorActionPreference = $previous }
    if ($dockerExit -ne 0) { throw "Docker failed (exit $dockerExit)." }
}

function Invoke-WalletCompose {
    param([Parameter(Mandatory=$true)][string[]]$ComposeArguments)
    Invoke-WalletDocker -DockerArguments (@('compose','--project-name','walletledger','--project-directory',$script:WalletRoot,'--file',(Join-Path $script:WalletRoot 'compose.yml'),'--env-file',(Join-Path $script:WalletRoot '.env')) + $ComposeArguments)
}

function New-WalletSecret {
    $bytes = New-Object byte[] 32
    $random = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $random.GetBytes($bytes) } finally { $random.Dispose() }
    return ([BitConverter]::ToString($bytes)).Replace('-','').ToLowerInvariant()
}

function Initialize-WalletEnvironment {
    $config = Join-Path $script:WalletRoot '.env'
    if (Test-Path -LiteralPath $config) { return }
    $existing = & docker volume ls --filter 'label=com.docker.compose.project=walletledger' --format '{{.Name}}'
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect existing WalletLedger volumes.' }
    if ($existing) { throw 'WalletLedger data exists but .env is missing. Restore the original credentials.' }
    $databasePassword = New-WalletSecret
    $runtimePassword = New-WalletSecret
    $receiptSecret = New-WalletSecret
    $lines = @(
        "WALLETLEDGER_DB_PASSWORD=$databasePassword"
        "WALLETLEDGER_RUNTIME_PASSWORD=$runtimePassword"
        "WALLETLEDGER_RECEIPT_SECRET=$receiptSecret"
        "DATABASE_URL=postgresql://walletledger_runtime:${runtimePassword}@127.0.0.1:5462/walletledger"
        "WALLETLEDGER_MIGRATION_URL=postgresql://walletledger:${databasePassword}@127.0.0.1:5462/walletledger"
        "WALLETLEDGER_TEST_ADMIN_URL=postgresql://walletledger:${databasePassword}@127.0.0.1:5462/postgres"
        'WALLETLEDGER_ALLOWED_ORIGINS=http://127.0.0.1:8170,http://localhost:8170,http://127.0.0.1:8171,http://127.0.0.1:5175,http://localhost:5175'
        'WALLETLEDGER_COOKIE_SECURE=false'
    )
    [IO.File]::WriteAllLines($config,$lines,(New-Object Text.UTF8Encoding($false)))
}

function Import-WalletEnvironment {
    $config = Join-Path $script:WalletRoot '.env'
    if (-not (Test-Path -LiteralPath $config)) { throw 'Run scripts/Start.ps1 to initialize .env.' }
    $names = @('WALLETLEDGER_DB_PASSWORD','WALLETLEDGER_RUNTIME_PASSWORD','WALLETLEDGER_RECEIPT_SECRET','DATABASE_URL','WALLETLEDGER_MIGRATION_URL','WALLETLEDGER_TEST_ADMIN_URL','WALLETLEDGER_ALLOWED_ORIGINS','WALLETLEDGER_COOKIE_SECURE','WALLETLEDGER_FRONTEND_DIR')
    foreach ($line in [IO.File]::ReadAllLines($config)) {
        if ($line -match '^([A-Z_]+)=(.*)$' -and $names -contains $matches[1]) { [Environment]::SetEnvironmentVariable($matches[1],$matches[2],'Process') }
    }
    $privateTemp = Join-Path $script:WalletRoot '.local/tmp'
    New-Item -ItemType Directory -Path $privateTemp -Force | Out-Null
    $env:TEMP=$privateTemp; $env:TMP=$privateTemp; $env:GOTMPDIR=$privateTemp
    $cacheRoot=Join-Path $script:WalletRoot '.local/cache'
    if(Test-Path -LiteralPath 'D:/DevCache') { $cacheRoot='D:/DevCache' }
    New-Item -ItemType Directory -Path $cacheRoot -Force | Out-Null
    $env:GOCACHE=Join-Path $cacheRoot 'go-build'; $env:GOMODCACHE=Join-Path $cacheRoot 'gomod'; $env:GOPATH=Join-Path $cacheRoot 'go-work'
    $env:GOTOOLCHAIN='local'; $env:GOMAXPROCS='2'
    $env:npm_config_cache=Join-Path $cacheRoot 'npm'; $env:PIP_CACHE_DIR=Join-Path $cacheRoot 'pip'
}

function Get-WalletGo {
    $portable = 'D:/DevTools/go1.27.3/go/bin/go.exe'
    if (Test-Path -LiteralPath $portable) { return $portable }
    $installed = Get-Command go -ErrorAction SilentlyContinue
    if ($installed) { return $installed.Source }
    throw 'Go 1.27.3 is required for native development. Docker startup compiles Go inside its build image.'
}
