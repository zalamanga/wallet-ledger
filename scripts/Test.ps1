. (Join-Path $PSScriptRoot 'Common.ps1')
Import-WalletEnvironment
$go = Get-WalletGo
Push-Location (Join-Path $script:WalletRoot 'backend')
try { & $go test -count=1 -p 1 -parallel 1 -timeout 12m ./...; if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL integration tests failed.' } } finally { Pop-Location }
Push-Location $script:WalletRoot
try {
    & npm.cmd --prefix frontend test -- --maxWorkers=1 --minWorkers=1
    if ($LASTEXITCODE -ne 0) { throw 'Frontend unit tests failed.' }
    & npm.cmd --prefix frontend run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
} finally { Pop-Location }
