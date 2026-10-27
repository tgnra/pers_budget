# Hashes accounts.txt into accounts_gen.go, then builds budget.exe.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
go generate
if ($LASTEXITCODE) { exit $LASTEXITCODE }
go build -o budget.exe .
if ($LASTEXITCODE) { exit $LASTEXITCODE }
Write-Host 'Built budget.exe'
