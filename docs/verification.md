# SSHDesk MVP 검증 기록

검증일: 2026-09-21. 실제 회사 SSH 서버, 기존 키 파일, 사용자 SSH config는 변경하지 않았습니다. SSH 통합 검증에는 테스트가 생성한 임시 키와 loopback SSH 서버를 사용했습니다.

## 환경 / 빌드

- Go 1.27.1, Linux amd64 (WSL Ubuntu 26.04), Windows amd64
- Linux native build: `dist/sshdesk`
- Windows native executable: `sshdesk.exe`, 15,854,592 bytes (약 15.1 MiB)
- Windows SHA256: `42ED59BEF6ED224293CA23C5EA80127E0BA8BFA597D0F7A67B96A052C06532FF`
- CGO disabled Windows build, SQLite / HTML / CSS / JavaScript / xterm.js 내장
- Node.js는 개발 중 문법 확인/포맷에만 사용했습니다. 프로젝트 실행·빌드에 npm 설치나 프런트엔드 빌드는 필요하지 않습니다.

## 자동 검증

| 검증 | 결과 |
|---|---|
| `go test ./...` | 26개 최상위 테스트 통과 (RSA PEM 및 백업 검증 하위 테스트 포함) |
| `go test -race -count=1 ./...` | 전체 통과, race 보고 없음 |
| `go vet ./...` | 통과 |
| JavaScript syntax check | 통과 |
| Windows 테스트 실행파일 | 6개 패키지, 25개 최상위 테스트 통과 (추가된 백업 테스트 포함) |
| Windows `key.test.exe` | OS 애플리케이션 제어 정책으로 실행 차단. 보호 설정 변경/우회 안 함 |
| `govulncheck v1.8.0` | 사용 중인 패키지 및 호출 경로의 알려진 취약점 0건 |

Windows에서 통과한 패키지는 `database`, `model`, `sshconfig`, `sshclient`, `tunnel`, `web`입니다. 키 검증 단독 테스트는 Linux에서 통과했고, Windows에서는 SSH 통합 테스트의 키 인증, HTTP key import 및 실제 UI의 ED25519 PEM 파일 등록을 확인했습니다. 따라서 Windows의 모든 단독 테스트가 실행됐다고 주장하지 않습니다.

취약점 스캐너는 의존 모듈 `golang.org/x/crypto` 내의 미사용 `openpgp` 패키지에 대한 `GO-2026-5932`도 표시했습니다. SSHDesk는 해당 패키지를 import/호출하지 않습니다. 이 결과는 점검 시점의 알려진 취약점 검사이며 향후 취약점 부재를 보증하지 않습니다.

## SSH / Tunnel / Terminal 통합 검증

- 서로 다른 서버 키를 가진 로컬 SSH 서버 3대에서 **2단계 Jump** 연결
- ED25519 키 인증, host fingerprint / known_hosts 검증 및 불일치 차단
- PTY shell, UTF-8 한글, ANSI, stdout, stderr, Ctrl+C 입력, Ctrl+D 종료
- SSH 및 WebSocket을 통한 터미널 크기 변경
- Jump 연결을 통한 실제 TCP echo 포워딩
- 로컬 포트 점유 감지, 연결 실패 시 포트 반환
- 모의 연결 장애 후 재연결, Auto Reconnect off, 초기 연결 중 Stop 취소
- 서비스 Open에서 터널 자동 시작 + 원격 TCP readiness + 브라우저 실행 함수 호출
- 서비스 목적지에 도달할 수 없으면 브라우저 실행 차단
- HTTP Host/Origin/CSRF/Fetch Metadata, WebSocket Origin/token 거부
- config Preview/선택 import/누락 Jump 원자적 롤백 및 원본 불변
- 설정 JSON Export/Import roundtrip, preview 무변경, 중복 재가져오기, ID 충돌/잘못된 참조 원자적 거부, 알 수 없는 secret 필드 거부, PC 경로 설정 opt-in

## 실제 프로세스 실행

- **Windows**: `sshdesk.exe` 실행 → 기본 AppData SQLite 생성 → `/health` HTTP 200과 `{"app":"SSHDesk","status":"ok"}` 확인.
- Windows TCP listener 조회 결과 **127.0.0.1:9876만** listen. 외부 인터페이스 listen 없음.
- 기본 실행 시 브라우저 열기 경로 실행, `-no-browser` 실행도 확인.
- 두 번째 인스턴스는 포트 충돌로 즉시 종료하며 DB를 열지 않음.
- **Linux**: `dist/sshdesk -no-browser -port 9877` 실행 및 동일 health 응답 확인. SIGINT로 테스트 프로세스 종료.

## 브라우저 UI 검수

Windows의 실제 에이전트를 브라우저에서 열어 Dashboard를 시각적으로 확인했습니다. 별도 테스트 데이터 디렉터리에서 임시 키 등록, Host 등록/STG 표시, Tunnel 등록, Service 카드 생성, Terminal 선택, xterm 렌더링, 연결 거절 메시지 및 Reconnect/Disconnect UI를 확인했습니다. 해당 화면에서 브라우저 JavaScript error/warn은 없었습니다.

검수 중 터미널 오류 글자 대비, 중복 상태 표시 및 연결 오류 상태 보존을 수정했습니다. 최종 사용자 데이터 디렉터리는 테스트 항목 없이 초기화된 상태입니다.

## 발견 및 제한

- Windows SQLite DB를 WSL UNC 공유 경로에 두면 `SQLITE_BUSY`가 발생했습니다. Windows 기본 데이터 위치는 로컬 `%LOCALAPPDATA%\SSHDesk`를 사용합니다. 네트워크 공유 DB는 지원하지 않습니다.
- 사내 Bastion 방화벽/보안그룹, 실제 계정 권한, 조직의 서버 키 정책은 실서버에 접속하지 않아 검증하지 않았습니다.
- 암호화된 키 입력, SSH agent, SFTP, Remote/SOCKS 포워딩 및 팀 권한 관리는 현재 범위 밖입니다. 자세한 Roadmap과 보안 모델은 README를 참고하세요.
