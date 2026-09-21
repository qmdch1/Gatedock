#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
GO="${GO:-go}"
"$GO" vet ./...
"$GO" test -race -count=1 -cover ./...
"$GO" build -trimpath -o dist/sshdesk ./cmd/sshdesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 "$GO" build -trimpath -ldflags='-s -w' -o dist/sshdesk.exe ./cmd/sshdesk
mkdir -p .test-data/windows-tests
for pkg in model database key sshconfig sshclient tunnel web; do
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 "$GO" test -c -o ".test-data/windows-tests/$pkg.test.exe" "./internal/$pkg"
done
