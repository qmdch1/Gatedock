$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'go test failed' }
    go build -trimpath -ldflags '-s -w' -o sshdesk.exe ./cmd/sshdesk
    if ($LASTEXITCODE -ne 0) { throw 'build failed' }
    Get-FileHash -Algorithm SHA256 -LiteralPath './sshdesk.exe'
} finally { Pop-Location }
