# SSHDesk

[![Windows용 SSHDesk 다운로드](docs/images/download-windows.svg)](https://github.com/qmdch1/Gatedock/releases/latest/download/sshdesk.exe)

**[⬇ Windows용 sshdesk.exe 다운로드](https://github.com/qmdch1/Gatedock/releases/latest/download/sshdesk.exe)**

## 개요

내부망에서 개발하다 보면 서버 접속은 SSH 클라이언트에서, Bastion 경유와 포트 포워딩은 별도 명령어로, 내부 서비스 접속은 브라우저에서 처리하게 됩니다. 여러 도구를 오가며 접속 정보를 반복해서 설정하는 불편함을 줄이기 위해 SSHDesk를 만들었습니다.

SSHDesk는 Windows에서 SSH 접속, 다중 Bastion 경유, 터널, 내부 웹 서비스 바로가기를 한곳에서 관리하는 도구입니다. 실행파일 하나로 시작하고, 브라우저에서 서버를 등록하거나 터미널을 열고 필요한 서비스를 실행할 수 있습니다.

연결 구성을 설정파일로 내보내 팀원에게 공유할 수도 있습니다. 팀원은 파일을 가져와 같은 호스트·경유 경로·터널·서비스 구성을 재사용하고, 자신의 SSH 키와 접속 권한에 맞춰 사용하면 됩니다.

## 주요 기능

- **서버 관리** — 호스트와 SSH 키를 등록하고 환경별로 관리
- **Bastion / 다중 경유** — Jump Host를 연결해 내부 서버에 접근
- **웹 터미널** — 등록한 서버에 브라우저에서 SSH 접속
- **터널 관리** — 로컬 포트 포워딩 시작·중지 및 연결 끊김 시 자동 재연결
- **웹 바로가기** — Tunnels에서 필요한 연결을 시작하고 내부 웹 서비스를 열기
- **기존 SSH 설정 활용** — 실행 시 Windows SSH config의 새 호스트 자동 등록
- **설정파일 공유** — 연결 구성을 내보내 다른 PC나 팀원에게 전달

## 화면 미리보기

가상 데이터로 촬영한 화면입니다.

![대시보드](docs/images/dashboard.jpg)

Tunnels에서 포트 연결과 웹 바로가기를 함께 관리합니다. 기존 Services 항목도 해당 터널의 바로가기로 표시됩니다.

![터널과 웹 바로가기](docs/images/tunnels.jpg)

## 시작하기

1. [Windows용 sshdesk.exe 다운로드](https://github.com/qmdch1/Gatedock/releases/latest/download/sshdesk.exe) 후 실행합니다.
2. 자동으로 열리는 브라우저에서 관리 화면에 접속합니다. 직접 접속할 때는 <http://127.0.0.1:9876>을 사용합니다.
3. 기존 `%USERPROFILE%\.ssh\config`가 있으면 새 호스트와 키 경로를 자동으로 등록합니다. 직접 등록하려면 **Keys → Add key**, **Hosts → Add host**를 사용합니다.
4. 호스트의 **Terminal**로 접속하거나, **Tunnels → Add tunnel**에서 포트 연결을 등록합니다. 웹 서비스라면 선택 항목인 웹 바로가기 이름과 URL도 입력하세요.

개인 키는 파일 선택 또는 경로 입력으로 등록합니다. Bastion을 거치는 서버는 Host의 **Jump host**에 경유할 호스트를 지정하세요. 서버 확인에는 해당 PC의 `known_hosts` 또는 별도로 확인한 서버 지문을 사용합니다.

프로그램을 종료하면 SSH 연결과 터널도 종료됩니다. 설정은 `%LOCALAPPDATA%\SSHDesk\sshdesk.db`에 남으므로 실행파일을 교체하거나 다시 실행해도 유지됩니다.

## 팀원과 설정 공유하기

서버와 서비스 구성을 한 번 등록한 뒤 설정파일로 공유하면, 팀원마다 같은 내용을 다시 입력할 필요가 없습니다.

1. **Settings → Configuration backup → 내보내기**로 설정파일을 저장합니다.
2. 팀 내부의 승인된 공유 경로로 파일을 전달합니다.
3. 받는 팀원은 **설정파일 선택 → Preview**에서 가져올 내용을 확인합니다.
4. **Import configuration**으로 가져온 뒤 자신의 SSH 키 경로와 접속 계정을 확인합니다.

호스트, Jump Host 관계, 터널, 서비스 및 키 파일 경로를 공유합니다. **개인 키 파일과 비밀번호는 포함되지 않으며, 접속 권한도 부여하지 않습니다.** 각 팀원은 자신의 키와 서버 접속 권한이 필요합니다. 이 PC의 SSH config·known_hosts 경로 설정은 가져오기로 바뀌지 않습니다.

가져오기는 기존 항목을 덮어쓰지 않습니다. 동일 항목은 건너뛰고, 충돌이 있으면 오류를 표시합니다. 설정파일에는 내부 주소와 계정명이 포함되므로 공개 저장소에는 올리지 마세요.

## 로컬망 다운로드 페이지

Settings 아래 **팀 공유 → 공유 시작**을 누르면 같은 로컬망의 팀원이 `http://공유-PC-IP:9877`에 접속해 실행파일과 설정파일을 받을 수 있습니다. 관리 화면과 터미널은 공유되지 않습니다.

키까지 자동 설정하려면 **포함할 SSH 키 선택 → 선택한 키로 배포본 만들기 → 공유 시작** 순서로 사용합니다. 선택한 키를 사용하는 호스트와 관련 터널·웹 바로가기가 포함됩니다. 경유 호스트에 다른 키가 필요하면 해당 키도 직접 선택해야 합니다. 받는 PC에서 다운로드한 실행파일을 처음 실행하면 개인 키를 사용자 전용 데이터 폴더에 저장하고 연결 구성을 등록합니다. 기존 설정은 덮어쓰지 않으며 SSH에 자동 접속하지 않습니다. 서버 지문 또는 known_hosts 확인은 별도로 필요할 수 있습니다.

**키 포함 배포본은 개인 키 원문을 포함합니다.** 신뢰하는 팀원에게만 전달하고 공개 저장소에 올리지 마세요. 공유 페이지는 HTTP이므로 신뢰하는 로컬망에서만 사용하세요. 공유 중지나 앱 종료 시 준비한 배포본은 폐기되지만 이미 다운로드한 파일은 회수되지 않습니다. 일반 JSON 설정과 GitHub 릴리스에는 개인 키가 포함되지 않습니다. 설정을 수정한 뒤에는 배포본을 다시 만드세요.

접속 주소는 기본 통신 경로의 IP를 우선 표시합니다. **다른 네트워크 주소**에는 WSL·VPN 등 다른 어댑터 주소가 표시될 수 있습니다.

**Mac 팀원도 사용할 수 있습니다.** 공유 페이지에서 **macOS 설정 묶음 다운로드**를 선택하세요. ZIP에는 SSH 설정, `install.command`, 호스트별 SSH·터널 연결 명령이 담긴 `README.txt`가 들어 있습니다. ZIP을 풀고 해당 폴더에서 `bash install.command`를 실행한 뒤 안내된 명령으로 접속합니다. 기존 `~/.ssh/config`와 `known_hosts`는 변경하지 않습니다. Mac용 GUI 앱이 아닌 macOS 기본 SSH용 설정입니다.

키 포함 배포본을 준비했다면 같은 키가 Mac ZIP에도 포함됩니다. 준비하지 않았다면 README 안내에 따라 본인의 키를 넣어야 합니다. 설정 텍스트와 설치·접속 명령 안내만 따로 다운로드할 수도 있습니다. 서버 지문 확인은 유지하며, 터널은 실행한 터미널에서 Ctrl+C로 종료합니다.

Windows 방화벽에 다음 인바운드 규칙이 필요합니다. 관리자 PowerShell에서 실행합니다.

```powershell
New-NetFirewallRule -Name "SSHDesk-Team-Downloads-9877" -DisplayName "SSHDesk Team Downloads (Local Subnet)" -Direction Inbound -Action Allow -Protocol TCP -LocalPort 9877 -RemoteAddress LocalSubnet -Profile Domain,Private,Public
```

공유는 기본적으로 꺼져 있습니다. 앱을 종료하면 공유도 종료되며, 실행할 때부터 켜려면 `sshdesk.exe -share`를 사용하세요. 회사 네트워크의 장치 간 통신 제한이 있으면 별도 네트워크 허용이 필요할 수 있습니다.

## 실행 옵션

```powershell
# 기본 실행
.\sshdesk.exe

# 브라우저 자동 열기 끄기
.\sshdesk.exe -no-browser

# 기존 SSH config 자동 등록 끄기
.\sshdesk.exe -no-auto-import

# 별도 포트와 데이터 위치 사용
.\sshdesk.exe -port 9877 -data-dir C:\SSHDesk-workspace
```

데이터 위치는 로컬 디스크의 절대 경로를 사용하세요. 같은 데이터 위치를 여러 실행 인스턴스에서 동시에 사용하지 마세요.

## 지원 범위

Windows를 우선 지원하며, 실행 시 별도의 SSH 클라이언트나 Node.js 설치가 필요하지 않습니다. 관리 화면과 터널은 로컬 PC에서만 접근할 수 있고, 명시적으로 켠 다운로드 페이지만 같은 로컬망에 공유됩니다.

현재 암호가 설정된 개인 키, SSH agent, 하드웨어 키, SSH 인증서, SFTP, 원격 포워딩 및 SOCKS는 지원하지 않습니다. SSH config의 일부 고급 설정도 지원하지 않으며 가져오기 화면에서 확인할 수 있습니다. 팀 공유는 설정파일 전달 방식이며 중앙 계정·권한 관리 기능은 제공하지 않습니다.

## 소스에서 빌드하기

Go 버전과 의존성은 [go.mod](go.mod)를 참고하세요.

```powershell
go test ./...
go vet ./...
go build -o sshdesk.exe ./cmd/sshdesk
```

Windows 빌드 스크립트는 [scripts/build.ps1](scripts/build.ps1), 검증 결과는 [검증 기록](docs/verification.md)을 참고하세요.

## 오픈소스 라이선스

SSH, SQLite, WebSocket, 터미널에 사용한 라이브러리의 고지와 라이선스는 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)와 [licenses](licenses)에 있습니다.
