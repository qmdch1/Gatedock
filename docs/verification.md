# SSHDesk MVP 검증 기록

검증일: 2026-09-21. 실제 회사 SSH 서버, 기존 키 파일, 사용자 SSH config는 변경하지 않았습니다. SSH 통합 검증에는 테스트가 생성한 임시 키와 loopback SSH 서버를 사용했습니다.

## 환경 / 빌드

- Go 1.27.1, Linux amd64 (WSL Ubuntu 26.04), Windows amd64
- Linux native build: `dist/sshdesk`
- Windows native executable: `sshdesk.exe`, 15,878,144 bytes (약 15.1 MiB)
- Windows SHA256: `AE53827FC5D4DDB5D8A224477CCF4A4CD8C12CE1E4F69B7CF241E3A485D7618B`
- CGO disabled Windows build, SQLite / HTML / CSS / JavaScript / xterm.js 내장
- Node.js는 개발 중 문법 확인/포맷에만 사용했습니다. 프로젝트 실행·빌드에 npm 설치나 프런트엔드 빌드는 필요하지 않습니다.

## 자동 검증

선택 키 포함 팀 실행파일: 가상 ED25519 키로 선택 범위 제한, 경유 키 명시 선택, 실행파일 payload 왕복 및 기존 payload 제거, 최초 자동 설치, 재실행 시 로컬 수정 보존, 충돌 시 DB/키 파일 롤백, JSON에 키 원문 미포함, 공유 중지 시 준비 데이터 폐기를 검증했습니다. 전체 Go 테스트·vet·race와 Windows 교차 빌드를 통과했습니다. 가상 데이터 UI에서 키 선택·호스트 미리보기·배포본 준비를 확인했습니다. Windows 테스트 실행은 이 PC의 애플리케이션 제어 정책에 차단되어 Windows ACL 및 번들 EXE 최초 실행의 실제 검증은 완료하지 못했습니다. 실사용 개인 키로 배포본을 생성하거나 외부에 전송하지 않았습니다.

팀 공유: 전체 Go 테스트·vet·race와 Windows 빌드 통과. 별도 다운로드 리스너에서 관리 API/WebSocket 접근 거부, 쓰기 요청 거부, 외부 서브넷·잘못된 Host·cross-site 요청 거부 및 설정 백업 내용 검사를 통과했습니다. Linux 가상 데이터 환경에서 공유 시작·다운로드 페이지·설정파일 다운로드를 확인했습니다. 이 PC에서는 방화벽 스크립트가 PowerShell 서명 정책에, 새 Windows 실행파일은 애플리케이션 제어 정책에 차단되어 실제 Windows LAN 접속은 검증하지 못했습니다.

Tunnels 통합: Services 메뉴를 제거하고 기존 서비스 레코드를 터널의 웹 바로가기로 표시합니다. 전체 Go 테스트·vet·race 검사와 Windows 교차 빌드가 통과했습니다. 여러 바로가기 저장, URL 포트 검증 실패 시 롤백, 기존 API 수정 시 바로가기 보존, 터널 삭제 시 관련 바로가기 삭제를 테스트했습니다. 격리된 가상 데이터 화면에서 기존 바로가기 표시·편집·저장을 확인했습니다. 새 Windows 실행파일의 smoke 실행은 Windows 애플리케이션 제어 정책에 차단되었습니다. 실제 사용 중인 터널은 중단하지 않았습니다.

서비스의 호스트 직접 선택 추가: 전체 Go 테스트, vet, race 테스트, JavaScript 문법 검사 및 Windows 빌드 성공. 터널·서비스 동시 저장, 저장 시 미연결, 잘못된 URL 포트의 원자적 롤백, 없는 호스트 거부를 검증했습니다. 새 Windows 실행파일의 격리된 smoke 실행은 도구 정책에 차단되어 완료하지 못했습니다. 이번 빌드는 `dist/sshdesk.exe`에 있으며 위 루트 실행파일 해시와 별개입니다.

| 검증 | 결과 |
|---|---|
| `go test ./...` | 31개 최상위 테스트 통과 (자동 SSH 등록, RSA PEM 및 백업 검증 하위 테스트 포함) |
| `go test -race -count=1 ./...` | 전체 통과, race 보고 없음 |
| `go vet ./...` | 통과 |
| JavaScript syntax check | 통과 |
| Windows 기존 MVP/백업 테스트 | 이전 빌드에서 6개 패키지, 25개 최상위 테스트 통과 |
| Windows 자동 등록 변경부 | `sshconfig` 8개 통과 (신규 자동 등록 테스트 5개 포함) |
| Windows 실행 제한 | 기존 `key.test.exe`, 이번 빌드 `web.test.exe` 재실행이 OS 애플리케이션 제어 정책으로 차단됨. 보호 설정 변경/우회 안 함 |
| `govulncheck v1.8.0` | 사용 중인 패키지 및 호출 경로의 알려진 취약점 0건 |

Windows에서 통과한 패키지는 `database`, `model`, `sshconfig`, `sshclient`, `tunnel`, `web`입니다. 키 검증 단독 테스트는 Linux에서 통과했고, Windows에서는 SSH 통합 테스트의 키 인증, HTTP key import 및 실제 UI의 ED25519 PEM 파일 등록을 확인했습니다. 따라서 Windows의 모든 단독 테스트가 실행됐다고 주장하지 않습니다.

자동 등록 변경 후 전체 회귀/race 검사는 Linux에서 통과했습니다. Windows에서는 갱신된 `sshconfig` 테스트 실행파일과 실제 앱 시작·자동 등록·Settings 결과 표시를 확인했습니다. 갱신된 웹 테스트 실행파일은 OS 정책으로 재실행하지 못했으며, 위 이전 빌드 결과와 구분합니다.

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
- 시작 시 SSH config 자동 등록: BOM, 다중 Jump, 재실행 중복 방지, 기존 편집 보존, 키 경로 재사용, 잘못된 항목/누락 의존성 건너뛰기, 순환 그룹 롤백, 파일 누락/파싱 오류 비치명 처리, 원본 불변

## 실제 프로세스 실행

- **Windows**: `sshdesk.exe` 실행 → 기본 AppData SQLite 생성 → `/health` HTTP 200과 `{"app":"SSHDesk","status":"ok"}` 확인.
- Windows TCP listener 조회 결과 **127.0.0.1:9876만** listen. 외부 인터페이스 listen 없음.
- 기본 실행 시 브라우저 열기 경로 실행, `-no-browser` 실행도 확인.
- 두 번째 인스턴스는 포트 충돌로 즉시 종료하며 DB를 열지 않음.
- **Linux**: `dist/sshdesk -no-browser -port 9877` 실행 및 동일 health 응답 확인. SIGINT로 테스트 프로세스 종료.

## 브라우저 UI 검수

키 파일 선택 버튼 추가: Linux의 web/platform 테스트와 vet, JavaScript 문법 검사, Windows 빌드를 통과했습니다. 주입한 선택 함수로 한글·공백 경로, 취소, 오류, 중복 창 차단, CSRF 차단을 검증했습니다. Windows 재시작 후 브라우저에서 버튼 배치를 확인했습니다. 네이티브 파일 창에서 실제 파일을 선택하는 수동 검증은 수행하지 않았습니다.

밝은 관리 화면 테마 변경 후 Windows 실행파일을 다시 빌드하고 재시작했습니다. 브라우저에서 대시보드와 Host 추가 창의 흰색 표면, 진한 글자, 입력창 테두리를 시각적으로 확인했습니다. 터미널 출력 영역의 어두운 배경은 유지했습니다.

Windows의 실제 에이전트를 브라우저에서 열어 Dashboard를 시각적으로 확인했습니다. 별도 테스트 데이터 디렉터리에서 임시 키 등록, Host 등록/STG 표시, Tunnel 등록, Service 카드 생성, Terminal 선택, xterm 렌더링, 연결 거절 메시지 및 Reconnect/Disconnect UI를 확인했습니다. 해당 화면에서 브라우저 JavaScript error/warn은 없었습니다.

검수 중 터미널 오류 글자 대비, 중복 상태 표시 및 연결 오류 상태 보존을 수정했습니다. 테스트 항목은 격리된 데이터 디렉터리에서만 사용했습니다. 자동 등록 기능은 실제 앱 시작 및 Settings 결과 표시까지 확인했으며, 개인 Host 이름·주소·키 경로는 이 문서와 저장소에 포함하지 않습니다.

## 발견 및 제한

- Windows SQLite DB를 WSL UNC 공유 경로에 두면 `SQLITE_BUSY`가 발생했습니다. Windows 기본 데이터 위치는 로컬 `%LOCALAPPDATA%\SSHDesk`를 사용합니다. 네트워크 공유 DB는 지원하지 않습니다.
- 사내 Bastion 방화벽/보안그룹, 실제 계정 권한, 조직의 서버 키 정책은 실서버에 접속하지 않아 검증하지 않았습니다.
- 암호화된 키 입력, SSH agent, SFTP, Remote/SOCKS 포워딩 및 팀 권한 관리는 현재 범위 밖입니다. 자세한 Roadmap과 보안 모델은 README를 참고하세요.

Configuration backup 간소화: JavaScript 문법 검사와 Windows 빌드 성공. 새 실행파일은 Windows 애플리케이션 제어 정책에 의해 실행이 차단되어 이번 변경의 브라우저 시각 검수는 수행하지 못했습니다.
