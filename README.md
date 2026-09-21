# SSHDesk

Windows에서 개발자가 SSH, Bastion, 다중 Jump Host, Local Port Forwarding을 로컬 웹 UI로 관리하는 단일 실행파일 애플리케이션입니다. 외부 `ssh.exe`, Node.js, React, CDN 없이 동작합니다.

## 빠른 시작 / Windows 설치

1. 빌드된 `sshdesk.exe`를 로컬 폴더에 둡니다. 별도 설치나 관리자 권한은 필요하지 않습니다.
2. 실행하면 SQLite를 초기화하고 `127.0.0.1:9876`에서 서버를 시작합니다.
3. `/health` 확인 후 기본 브라우저가 자동으로 열립니다.
4. **Keys → Add key**, **Hosts → Add host** 순서로 등록하세요. 기존 SSH config는 **Settings → Import preview**로 가져올 수 있습니다.

```powershell
.\sshdesk.exe
```

브라우저 주소: <http://127.0.0.1:9876>. `localhost` 등 다른 Host 헤더는 허용하지 않습니다. 프로그램을 종료하면 연결과 터널도 종료됩니다. 브라우저 탭만 닫으면 해당 터미널은 종료되지만 터널은 계속 실행됩니다.

기본 데이터 위치는 `%LOCALAPPDATA%\SSHDesk\sshdesk.db`입니다. 사용자 `.ssh\config`와 `.ssh\known_hosts`는 읽기만 하며 자동 수정하지 않습니다. DB에는 키 **경로**와 연결 메타데이터만 저장합니다. OS 로그인 계정이 접근할 수 있는 SSH 키가 필요합니다.

```powershell
# 브라우저를 자동 실행하지 않기
.\sshdesk.exe -no-browser
# 다른 로컬 HTTP 포트 / 로컬 데이터 디렉터리
.\sshdesk.exe -port 9877 -data-dir C:\Users\dev\AppData\Local\SSHDesk-alt
```

`-data-dir`는 절대 경로여야 합니다. SQLite 잠금 때문에 DB는 **로컬 디스크**에 두세요. Windows에서 `\\wsl.localhost\...`, SMB, 공유 드라이브를 DB 경로로 사용하지 마세요. 실행파일과 소스는 WSL 경로에 있어도 DB 기본 위치는 Windows AppData입니다. 동일 데이터 디렉터리를 여러 에이전트에서 동시에 사용하지 마세요.

## 개발 / Build

[Go 공식 배포판](https://go.dev/dl/)을 설치합니다. 이 프로젝트는 2026-09-21에 확인한 최신 stable **Go 1.27.1**로 빌드·검증했습니다. CGO 없는 SQLite 드라이버를 사용하므로 Windows 기본 빌드에는 C 컴파일러가 필요하지 않습니다.

```powershell
go run ./cmd/sshdesk
go test ./...
go vet ./...
go build -o sshdesk.exe ./cmd/sshdesk
# 또는 테스트 + 배포용 빌드
.\scripts\build.ps1
```

Linux에서 Windows 교차 빌드:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o sshdesk.exe ./cmd/sshdesk
# Linux race 검사, 양 플랫폼 빌드, Windows 테스트 실행파일 생성
./scripts/verify.sh
```

이 작업 환경에는 임시 개발 도구를 `.tools/go`에 설치했습니다. PATH에 Go가 없다면 WSL에서 `.tools/go/bin/go`로 실행할 수 있습니다. `.tools`, `dist`, `.test-data`, 런타임 DB와 실행파일은 버전 관리에서 제외합니다. 의존성 버전은 `go.mod`/`go.sum`에 고정되어 있습니다.

## Architecture / 프로젝트 구조

```text
Browser (html/template + Vanilla JS + embedded xterm.js)
  │ HTTP JSON / WebSocket / CSRF + Origin validation
  ▼
Go SSHDesk agent — 127.0.0.1:9876
  ├── Host / Key / Settings / Service APIs
  ├── SSH connector → Bastion → Jump → Target
  ├── Tunnel manager → 127.0.0.1:local_port → remote_host:remote_port
  ├── Terminal → SSH session + PTY
  └── SQLite: hosts / ssh_keys / tunnels / services /
              connection_history / settings

cmd/sshdesk/                    실행, health 확인, 브라우저 열기, 종료
internal/model/                모델, 입력/참조/Jump 검증
internal/database/             SQLite 저장소와 원자적 갱신
internal/database/migrations/  embed SQL migration, PRAGMA user_version
internal/key/                  파일 검증, signer, passphrase 확장 인터페이스
internal/sshclient/             SSH 연결, 서버 키 검증, 다중 Jump
internal/sshconfig/             제한된 OpenSSH config parser
internal/tunnel/                포워딩, 상태, Keep Alive, 재연결
internal/web/                   CRUD API, 보안, WebSocket terminal
internal/platform/              OS 데이터 경로와 기본 브라우저 실행
internal/testssh/               테스트 전용 로컬 SSH 서버
web/templates/                 Go HTML templates
web/static/                    CSS, Vanilla JS, vendored xterm.js
scripts/                       재현 가능한 검증/빌드
```

작은 구조를 유지하기 위해 Host/Key/Tunnel/Service CRUD를 하나의 저장소와 웹 계층에 모았습니다. SSH `Connector`/`Connection` 인터페이스로 터널과 연결 로직을 분리했습니다. SQLite는 버전별 migration을 적용하고 레코드마다 JSON을 저장합니다. 루트 작업공간 정책에 따라 일반 인덱스를 사용하며 참조/중복/순환 검사는 저장소 잠금 안에서 수행한 후 트랜잭션으로 반영합니다. 수동 DB 편집은 지원하지 않습니다.

## 메뉴와 사용 방법

### Dashboard

등록 Host, 최근 성공 상태인 Host, 활성 Tunnel, 등록 Service 수를 표시합니다. 최근 연결 및 오류 내역은 최대 200건 보존합니다. Reachable은 **마지막 SSH 연결 시도 결과**이며 지속적인 실시간 네트워크 검사가 아닙니다. Host의 Check로 갱신하세요. 터널 상태는 5초 간격으로 갱신합니다.

### Keys

Name과 private-key 파일 절대 경로 또는 `~/.ssh/...`를 입력합니다. 파일 존재, 읽기 권한, 일반 파일 여부, 1 MiB 크기 제한, 키 해석 가능 여부를 검사합니다. PEM, RSA, ED25519 및 OpenSSH private-key 형식을 지원합니다.

MVP에서는 **암호화된 키/passphrase 입력, SSH agent, 하드웨어 키, SSH 인증서**를 지원하지 않습니다. `PassphraseProvider` 인터페이스는 향후 메모리 내 일시 입력을 위한 확장점입니다. 암호화된 키를 등록하면 명시적인 오류가 표시됩니다. 기존 키를 자동 변환하거나 복호화하지 않습니다.

### Hosts / Jump Host

Name, Environment(`LOCAL / DEV / STG / PROD`), Type(`Bastion / Server / Database / Other`), Address, SSH Port, Username, SSH Key, Jump Host, Description을 설정합니다.

1. Bastion을 먼저 등록하고 Jump Host를 `Direct connection`으로 둡니다.
2. 내부 서버를 등록하면서 Jump Host에 Bastion을 선택합니다.
3. 추가 내부 서버의 Jump Host에 이전 서버를 선택하면 다중 Jump가 됩니다. 최대 16 hop, 순환과 누락 참조는 차단합니다.
4. **Check**는 TCP뿐 아니라 SSH 인증 및 서버 키를 검증합니다.
5. **Terminal**로 해당 호스트의 interactive shell에 바로 연결합니다.

서버 키는 Settings의 `known_hosts` 또는 Host의 **Verified server fingerprint**로 검증합니다. 지문 예시는 `SHA256:...` 형식입니다. 반드시 서버 관리자나 별도의 신뢰 경로로 확인한 값을 입력하세요. 알 수 없는 키가 표시하는 지문을 확인 없이 그대로 신뢰하면 중간자 공격 방어가 무력화됩니다. 지문이 지정되어 있으면 해당 Host는 지문 검증을 사용하고, 비어 있으면 `known_hosts`를 사용합니다. 변경된 키는 자동 수락하지 않습니다.

연결 중 Host/Key 정보를 수정해도 기존 SSH 연결은 그대로 유지됩니다. 변경 적용 시 연결을 종료하고 다시 시작하세요. 참조 중인 키/호스트/터널 삭제는 차단됩니다.

### Tunnels

예: `127.0.0.1:18080 → 192.0.2.10:8080 via STG Pipeline`.

1. Tunnel에서 이름, Local Port, Remote Host/Port, SSH Host를 지정합니다.
2. **Start**는 로컬 포트를 먼저 확보하고 SSH 인증을 완료한 후 Running으로 전환합니다.
3. 포트 충돌이나 SSH 연결 실패 시 오류를 표시하며 확보한 리소스를 정리합니다.
4. Keep Alive는 30초 간격, 응답 대기 최대 10초, 연속 3회 실패 시 연결 손실로 처리합니다.
5. Auto Reconnect가 켜져 있으면 1~30초 지수 backoff와 jitter로 재연결합니다. 꺼져 있으면 오류 상태에서 종료합니다.
6. **Stop**은 연결 중, 재연결 중에도 취소하고 리스너·스트림·SSH 연결을 닫습니다.

Local Host는 **127.0.0.1만** 허용합니다. 각 터널은 최대 128개의 동시 로컬 연결을 허용합니다. 편집/삭제하려면 먼저 Stop하세요. 앱 재시작 시 터널을 자동 실행하지 않습니다. Auto Reconnect는 실행 중 끊어진 SSH 연결에 적용되며 최초 Start 실패 후에는 원인을 수정하고 다시 Start해야 합니다.

OpenSSH `ExitOnForwardFailure`와 같이 Start는 로컬 포트 확보/SSH 연결 실패를 감지합니다. Remote 목적지는 실제 클라이언트가 접근할 때 SSH 서버 측에서 연결하므로, **Running만으로 DB/API가 정상임을 보장하지 않습니다.** 실패 원인은 Last error에 표시됩니다. Services의 Open은 Remote까지 별도로 TCP 연결을 확인합니다. 재연결 후에는 기존 TCP 클라이언트/DB 세션을 새로 연결해야 합니다.

### Terminal

xterm.js와 Go SSH PTY를 WebSocket으로 연결합니다. stdin/stdout/stderr, UTF-8, ANSI 색상, Ctrl+C, Ctrl+D, 크기 변경을 지원합니다. 동시에 최대 16개의 터미널을 열 수 있습니다. Disconnect는 해당 shell을 종료하며 Reconnect는 **새 shell**을 만듭니다. 원래 shell 상태를 복원하지 않습니다. 장시간 작업은 원격 서버에서 별도로 `tmux` 등을 사용하세요.

PROD 연결은 확인 창, 붉은 환경 배지와 경고 배너를 표시합니다. 브라우저에서 다른 메뉴로 이동해도 현재 터미널은 유지됩니다. 다른 Host로 연결하면 기존 터미널을 종료합니다.

### Services

Name, Environment, Tunnel, URL, Description을 등록합니다. URL은 선택한 터널 Local Port와 일치하는 `http(s)://127.0.0.1:포트/...`만 허용합니다. URL에 사용자명/비밀번호를 포함할 수 없습니다.

**Open service → 필요하면 Tunnel Start → Remote TCP 연결 확인 → Windows 기본 브라우저 열기** 순서입니다. TCP 연결 성공은 HTTP 응답/DB 업무 상태 검사까지 의미하지 않습니다. 팝업 차단을 피하기 위해 Go 에이전트가 OS 기본 브라우저를 실행합니다.

### SSH Config Import

기본 `%USERPROFILE%\.ssh\config` 경로를 Settings에서 확인한 뒤 Import preview를 누릅니다. 가져올 Host 및 경유 Host를 선택하고 Import selected hosts를 누릅니다. 선택한 Host에 필요한 Key 경로도 함께 등록하며, 동일한 키 경로는 재사용합니다. 전체 선택을 한 트랜잭션으로 저장합니다. 중복 Host 이름, 누락된 Jump, 잘못된 키 파일은 전체 가져오기를 취소합니다.

지원: 명시적 Host 별칭(한 줄에 여러 별칭 가능), HostName, Port, User, IdentityFile, 별칭 기반 ProxyJump 및 쉼표로 구분한 다중 Jump, 주석, 따옴표, `Port=22`, wildcard 기본값과 부정 패턴, first-value-wins.

지원하지 않는 라우팅/인증 지시문은 Preview에서 경고하고 선택을 차단합니다: ProxyCommand, CertificateFile, IdentityAgent, 적용되는 Include, Match 조건이 있는 파일. Include 파일을 자동 탐색하거나 Match exec를 실행하지 않습니다. `%`/`${...}` 확장, `user@host:port` ProxyJump는 지원하지 않습니다. 복잡한 config는 원본을 보존한 채 **단순화한 별도 복사본**을 가져오거나 UI에서 등록하세요. Local/Remote/DynamicForward는 경고 후 가져오지 않으므로 Tunnels에서 직접 추가합니다. 가져온 환경은 DEV이므로 PROD/STG 여부를 확인하세요.

## Security

### 설정 Export / Import

Settings의 **Configuration backup**에서 **Export settings**로 `*.sshdesk.json` 파일을 다운로드합니다. Host, Tunnel, Service, 키 파일 **경로**, 앱 설정만 포함하며 개인 키 원문, 비밀번호, 연결 기록, `.ssh/config`와 `known_hosts` 파일 내용은 포함하지 않습니다. 데이터가 외부 서버로 업로드되지 않습니다.

**SSHDesk backup file → Import preview → Import configuration** 순서로 가져옵니다. 동일 ID·동일 내용은 건너뛰고 새 항목만 추가합니다. ID 내용 충돌, Host 이름 중복, 누락 참조, 순환 Jump 또는 유효하지 않은 설정이 있으면 전체 작업을 취소합니다. 기존 항목은 덮어쓰지 않습니다. SSH config/known_hosts 경로는 선택 항목을 체크해야 가져옵니다. 키 파일은 별도로 안전하게 옮기고 해당 PC의 경로를 수정하세요. 가져오기는 파일을 읽거나 SSH/터널을 자동 시작하지 않습니다. 최대 백업 크기는 UI에서 1 MiB입니다.

백업은 암호화되지 않은 JSON이며 내부 주소·사용자명·파일 경로를 포함합니다. 안전한 로컬 위치에 보관하세요. `*.sshdesk.json`, `exports/`, `backups/`, 로컬 DB, `.env`, 키 파일, 실행파일 및 테스트 데이터는 `.gitignore`로 제외합니다. 임의 이름으로 바꾼 민감 파일까지 Git이 자동 판별하지는 않으므로 공유 전 staged 파일을 확인해야 합니다.

- HTTP 및 터널은 IPv4 loopback 전용입니다. 외부 bind 옵션은 없습니다.
- HTTP Host allowlist, 동일 Origin 검사, cross-site Fetch Metadata 차단, 모든 변경 요청의 실행별 CSRF token, JSON-only API를 사용합니다.
- WebSocket은 동일 Origin과 첫 메시지의 CSRF token으로 인증합니다. token을 URL 쿼리에 넣지 않습니다.
- CSP, frame 차단, MIME sniffing 방지, no-referrer, no-store를 설정합니다. xterm.js/fit addon은 버전을 고정하고 embed합니다. 런타임 외부 다운로드/텔레메트리가 없습니다.
- SSH 서버 키 확인은 필수입니다. `InsecureIgnoreHostKey`를 사용하지 않습니다. private-key 원문, passphrase와 비밀번호를 DB/기록에 저장하지 않습니다.
- shell 명령 문자열로 SSH를 실행하지 않습니다. 기본 브라우저만 OS별 프로그램과 분리된 인자로 실행합니다.
- SQLite에는 내부 주소, 사용자명, 경로, 설명, 서비스 URL, 연결 오류가 평문으로 남습니다. Windows 사용자 계정과 AppData ACL, 키 파일 ACL을 보호하세요. DB 백업도 내부 정보입니다.
- 이 프로그램은 **신뢰할 수 있는 단일 사용자 PC**를 전제로 합니다. 같은 계정의 악성 프로세스나 브라우저 확장 프로그램을 격리하는 보안 경계가 아니며, 다중 사용자/RBAC 서버가 아닙니다. HTTP 리버스 프록시로 외부에 노출하지 마세요.
- 중단 시 TCP 세션은 종료됩니다. 실수 방지용 PROD 표시는 권한 통제가 아닙니다. 서버 측 최소 권한을 유지하세요.

## 테스트

`go test ./...`는 회사 서버나 실제 키 없이 실행됩니다. 로컬 테스트 SSH 서버에서 임시 키를 생성하며 테스트 종료 시 정리합니다.

- DB 생성/CRUD/재실행 영속성, 참조 차단, 실패 롤백, history 제한
- Host/Tunnel/Service URL 검증, Jump 순환 및 누락
- SSH config 따옴표/Windows 경로/default/unsupported parsing 및 선택 import
- 실제 SSH 인증, 2단계 Jump(3대 SSH 서버), shell UTF-8/ANSI/Ctrl+C, PTY resize, 포워딩
- 서버 키 pin/known_hosts 정상·미등록·변경 검사
- 포트 충돌, Start 실패 포트 반환, Stop 취소, 재연결, 재연결 비활성
- WebSocket Origin/첫 메시지 인증, 실제 SSH stdout/stderr/Ctrl+D
- Services 자동 시작, 원격 TCP readiness, 도달 불가 시 브라우저 실행 차단
- HTTP Host/Origin/CSRF/Fetch Metadata 및 embed 리소스

검증 결과와 플랫폼별 실제 실행 확인은 [검증 기록](docs/verification.md)을 참고하세요.

## Roadmap — 현재 미구현

- Passphrase 입력, SSH agent/하드웨어 키/인증서
- 완전한 OpenSSH config(Include/Match/고급 토큰) 호환
- Remote forwarding(-R), SOCKS(-D), SFTP/SCP
- Docker, systemd/PM2, Metrics, Log Viewer
- Team Host 공유, RBAC, Audit Log, Secrets Manager
- macOS/Linux 패키징 및 운영 검증, Windows tray/service/서명 배포
- Tailscale/WireGuard integration

Linux 실행 및 Windows amd64 빌드를 제공하지만 이번 MVP의 우선 검증 대상은 Windows입니다. 위 항목을 완료 기능으로 간주하지 않습니다.

## Third-party notices

- [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh)
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
- [coder/websocket](https://github.com/coder/websocket)
- [xterm.js](https://github.com/xtermjs/xterm.js), `@xterm/xterm` 6.0.0 / `@xterm/addon-fit` 0.11.0

프런트엔드 라이선스, 다운로드 원본과 SHA256은 `web/static/vendor`에 함께 포함했습니다. Go 의존성 고지와 라이선스 사본은 `THIRD_PARTY_NOTICES.md`, `licenses/`에 보관합니다.
