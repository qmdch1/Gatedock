# SSHDesk project

- Build a Windows-first, local-only SSH/Bastion/Tunnel desktop agent in Go. Embed HTML, CSS, JavaScript and xterm.js; no Node/React application or runtime CDN.
- Bind HTTP and local tunnels only to 127.0.0.1. Validate HTTP Host, Origin, CSRF tokens and WebSocket Origin. Verify SSH server keys using known_hosts or explicitly verified SHA256 fingerprints.
- Store private-key file paths only, never passwords, key contents or passphrases. Do not modify the user's SSH config or known_hosts automatically.
- Keep OS-specific browser launching and data directory selection in internal/platform. Use interfaces for SSH and tunnel tests.
- Test against isolated local SSH servers; do not connect to real company hosts without an explicit request. Run go test ./..., go vet ./..., Linux race tests and Windows build/smoke checks for connection changes.
- Preserve the seven MVP menus: Dashboard, Hosts, Tunnels, Terminal, Services, Keys, Settings. Do not add SFTP, logs or Docker management to this MVP.
- Database migrations use ordinary indexes; integrity and reference validation run under the store mutation lock, following the parent workspace schema policy.
- Configuration and SSH files stay local. Export/import must never include key bytes, passwords, SSH file contents or history; exports contain sensitive connection metadata and must be excluded from Git. Import previews additions, preserves conflicting existing records, applies atomically, and never starts connections.
- Before publishing to the project Git remote, inspect the staged file list and scan for credentials, private keys, local databases, exported configurations, internal endpoints and personal paths. Publish source and sanitized documentation only.
