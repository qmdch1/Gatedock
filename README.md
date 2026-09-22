# SSHDesk

## Windows에서 실행이 차단될 때

**Smart App Control이 SSHDesk 실행을 차단한 경우**, 해당 보호 기능을 끄려면 **PowerShell을 관리자 권한으로 실행**하고 아래 명령을 복사해 입력하세요.

```powershell
Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Control\CI\Policy" -Name "VerifiedAndReputablePolicyState" -Value 0
```

> 이 명령은 SSHDesk만 허용하는 것이 아니라 PC 전체의 Smart App Control을 끄는 설정입니다. 일반적인 프로그램 오류 해결 명령이 아니며, Windows 버전이나 관리 정책에 따라 적용되지 않을 수 있습니다. 회사 관리 PC에서는 관리자에게 확인하세요. Microsoft는 레지스트리를 통한 변경을 테스트 용도로 안내합니다. [Microsoft 공식 안내](https://learn.microsoft.com/en-us/windows/apps/develop/smart-app-control/test-your-app-with-smart-app-control#configure-smart-app-control-by-using-the-registry)

[![Windows용 SSHDesk 다운로드](docs/images/download-windows.svg)](https://github.com/qmdch1/Gatedock/releases/latest/download/sshdesk.exe)

**[⬇ Windows용 sshdesk.exe 다운로드](https://github.com/qmdch1/Gatedock/releases/latest/download/sshdesk.exe)**

## 개요

내부망에서 개발하다 보면 서버 접속은 SSH 클라이언트에서, Bastion 경유와 포트 포워딩은 별도 명령어로, 내부 서비스 접속은 브라우저에서 처리하게 됩니다. 여러 도구를 오가며 접속 정보를 반복해서 설정하는 불편함을 줄이기 위해 SSHDesk를 만들었습니다.

SSHDesk는 Windows에서 SSH 접속, 다중 Bastion 경유, 터널, 내부 웹 서비스 바로가기를 한곳에서 관리하는 도구입니다. 실행파일 하나로 시작하고, 브라우저에서 서버를 등록하거나 터미널을 열고 필요한 서비스를 실행할 수 있습니다.

한 사람이 구성한 접속 환경을 팀원에게 공유할 수 있습니다. Windows 팀원은 키와 설정이 담긴 실행파일을, Mac 팀원은 SSH 설정 묶음을 받아 같은 호스트와 터널을 사용할 수 있습니다.

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

Tunnels에서 포트 연결과 웹 바로가기를 함께 관리합니다.

![터널과 웹 바로가기](docs/images/tunnels.jpg)

호스트별 접속 주소, 계정, Jump Host를 한눈에 확인합니다.

![호스트와 경유 설정](docs/images/hosts-configured.jpg)

## 시작하기

1. [Windows용 sshdesk.exe 다운로드](https://github.com/qmdch1/Gatedock/releases/latest/download/sshdesk.exe) 후 실행합니다.
2. 자동으로 열리는 브라우저에서 관리 화면에 접속합니다. 직접 접속할 때는 <http://127.0.0.1:9876>을 사용합니다.
3. 기존 `%USERPROFILE%\.ssh\config`가 있으면 새 호스트와 키 경로를 자동으로 등록합니다. 직접 등록하려면 **Keys → Add key**, **Hosts → Add host**를 사용합니다.
4. 호스트의 **Terminal**로 접속하거나, **Tunnels → Add tunnel**에서 포트 연결을 등록합니다. 웹 서비스라면 선택 항목인 웹 바로가기 이름과 URL도 입력하세요.

개인 키는 파일 선택 또는 경로 입력으로 등록합니다. Bastion을 거치는 서버는 Host의 **Jump host**에 경유할 호스트를 지정하세요. 서버 확인에는 해당 PC의 `known_hosts` 또는 별도로 확인한 서버 지문을 사용합니다.

프로그램을 종료하면 SSH 연결과 터널도 종료됩니다. 설정은 `%LOCALAPPDATA%\SSHDesk\sshdesk.db`에 남으므로 실행파일을 교체하거나 다시 실행해도 유지됩니다.

## 팀원과 바로 시작하기

한 사람이 연결을 설정해두면 팀원은 다운로드해서 사용할 수 있습니다.

**공유하는 사람**

1. **팀 공유**에서 공유할 키를 선택합니다.
2. **선택한 키로 배포본 만들기 → 공유 시작**을 누릅니다.
3. 화면에 표시된 주소를 같은 로컬망의 팀원에게 전달합니다.

**받는 사람**

- **Windows:** 공유 주소 접속 → **프로그램 + 팀 접속 설정 받기 (추천)** → 실행. 키와 관련 호스트·터널이 자동 등록되고, 원본 PC의 추가·수정 사항도 30초마다 반영됩니다.
- **Mac:** 공유 주소 접속 → **macOS 설정 묶음 다운로드** → 압축 해제. 해당 폴더에서 `bash install.command`를 실행하고 `README.txt`의 접속 명령을 사용합니다.

개인이 만든 설정은 유지합니다. 공유받은 항목은 원본 기준으로 갱신하며, 원본에서 사라진 항목은 삭제하지 않고 회색 **원본에서 제거됨 · 보관** 표시로 남깁니다. 원본 PC의 공유와 받는 PC의 앱이 켜져 있어야 자동 갱신됩니다. 실행 중인 연결은 유지하고 다음 연결부터 변경 사항을 적용합니다.

기존 배포본 사용자는 새 팀 배포본을 한 번 받아 실행하세요. **팀 공유 → 내 PC의 원본 구독**에서 갱신을 일시 중지하거나 원본 IP 변경을 반영할 수 있습니다. 개인 키 추가·변경은 새 배포본이 필요합니다. Mac SSH 설정 묶음과 일반 JSON 가져오기는 자동 갱신하지 않으므로 변경 시 다시 받으세요.

처음 접속하는 서버의 지문은 확인이 필요할 수 있습니다. 키 포함 파일은 신뢰하는 팀원에게만 전달하고 공개 저장소에는 올리지 마세요.

![팀원용 Windows·Mac 다운로드 페이지](docs/images/team-download.jpg)

<details>
<summary>팀원이 공유 주소에 접속하지 못한다면</summary>

공유 PC에서 앱과 **팀 공유**가 켜져 있는지 확인하세요. 주소는 `http://공유-PC-IP:9877`이며, WSL·VPN 주소 대신 팀원과 연결된 로컬망 주소를 사용합니다.

Windows 방화벽 허용은 최초 한 번 관리자 PowerShell에서 실행합니다.

```powershell
New-NetFirewallRule -Name "SSHDesk-Team-Downloads-9877" -DisplayName "SSHDesk Team Downloads (Local Subnet)" -Direction Inbound -Action Allow -Protocol TCP -LocalPort 9877 -RemoteAddress LocalSubnet -Profile Domain,Private,Public
```

회사 네트워크에서 장치 간 통신을 제한하면 네트워크 관리자에게 확인하세요. HTTP 공유이므로 신뢰하는 로컬망에서만 사용하세요.

</details>

<details>
<summary>개인 키 없이 설정파일만 공유하기</summary>

**Settings → Configuration backup → 내보내기**로 파일을 전달합니다. 팀원은 **설정파일 선택 → Preview → Import configuration**으로 가져온 뒤 본인의 키 경로와 계정을 지정합니다.

</details>

## 실행 옵션

```powershell
# 기본 실행
.\sshdesk.exe

# 브라우저 자동 열기 끄기
.\sshdesk.exe -no-browser

# 기존 SSH config 자동 등록 끄기
.\sshdesk.exe -no-auto-import

# 별도 포트와 데이터 위치 사용
.\sshdesk.exe -port 9880 -data-dir C:\SSHDesk-workspace
```

데이터 위치는 로컬 디스크의 절대 경로를 사용하세요. 같은 데이터 위치를 여러 실행 인스턴스에서 동시에 사용하지 마세요.

## 지원 범위

Windows를 우선 지원하며, 실행 시 별도의 SSH 클라이언트나 Node.js 설치가 필요하지 않습니다. 관리 화면과 터널은 로컬 PC에서만 접근할 수 있고, 명시적으로 켠 다운로드 페이지만 같은 로컬망에 공유됩니다.

현재 암호가 설정된 개인 키, SSH agent, 하드웨어 키, SSH 인증서, SFTP, 원격 포워딩 및 SOCKS는 지원하지 않습니다. SSH config의 일부 고급 설정도 지원하지 않으며 가져오기 화면에서 확인할 수 있습니다. 팀 공유는 선택한 키 범위의 설정 배포와 Windows 앱 간 자동 갱신을 지원하며, 중앙 계정·권한 관리 기능은 제공하지 않습니다.

## Docker로 실행하기

Docker가 설치되어 있다면 아래 명령으로 **프로그램 빌드와 백그라운드 실행**을 한 번에 할 수 있습니다. SSH 키와 접속할 서버 정보는 본인이 준비해야 합니다.

```sh
git clone https://github.com/qmdch1/Gatedock.git
cd Gatedock
docker compose up -d --build
```

실행 후 브라우저에서 <http://127.0.0.1:9876>을 엽니다. 이미 실행 중인 Windows 프로그램이 같은 포트를 사용한다면 먼저 종료하세요. 설정은 Docker 볼륨에 저장되어 컨테이너를 다시 만들어도 유지됩니다.

- **Linux:** Docker Engine과 Compose가 필요합니다.
- **Windows / Mac:** Docker Desktop 4.34 이상에서 **Settings → Resources → Network → Enable host networking**을 켜야 합니다. Linux 컨테이너 모드를 사용하세요. 로컬 관리 화면과 터널을 연결하기 위해 host 네트워크를 사용합니다. [Docker 공식 안내](https://docs.docker.com/engine/network/drivers/host/)

```sh
# 실행 상태 확인
docker compose ps

# 종료 (저장된 설정은 유지)
docker compose down
```

<details>
<summary>Docker에서 SSH 키와 기존 접속 설정 사용하기</summary>

프로젝트의 `local/ssh` 폴더에 사용할 키와 필요한 `config`, `known_hosts` 파일을 넣으세요. 이 폴더는 컨테이너의 `/home/sshdesk/.ssh`에 읽기 전용으로 연결되며, Git과 이미지에서 제외됩니다. Linux에서는 컨테이너 사용자 UID 1000이 키를 읽을 수 있어야 합니다.

프로그램의 키 경로는 Windows 경로 대신 `/home/sshdesk/.ssh/키파일명`으로 입력합니다. `config`를 함께 넣으면 시작할 때 새 호스트를 읽어 등록합니다. `IdentityFile`에도 컨테이너에서 보이는 경로를 사용하세요. 기존 Windows 설정파일의 `C:\...` 경로는 그대로 사용할 수 없습니다.

다른 폴더를 연결하려면 실행 전에 `SSH_KEY_DIR` 환경변수에 그 폴더의 절대 경로를 지정합니다. 키를 Dockerfile에 복사하거나 이미지에 포함하지 마세요.

Docker에서는 Windows 파일 선택창과 키 포함 Windows 실행파일 배포를 지원하지 않습니다. SSH 키 경로는 직접 입력하고, 터널의 웹 페이지는 브라우저 주소창에 `http://127.0.0.1:설정한포트`를 입력해 여세요. Windows 팀 배포본을 만들려면 Windows 실행파일을 사용하세요.

</details>

## Docker 이미지를 클라우드에 보관하기

**가능합니다.** GitHub의 컨테이너 저장소인 **GHCR**에 이미지를 올리면 다른 PC나 클라우드 서버에서 받아 실행할 수 있습니다. 이미지는 프로그램만 담고, 개인 키·접속 설정·DB는 포함하지 않습니다.

이 저장소의 **Actions → Publish container image → Run workflow**를 실행하면 이미지를 빌드해 `ghcr.io/qmdch1/gatedock:latest`와 해당 커밋 태그로 올립니다. 자동 게시를 원하지 않을 때 실행되지 않도록 수동 실행 방식으로 구성했습니다. **현재 `ghcr.io/qmdch1/gatedock:latest` 이미지가 게시되어 있습니다.** [GitHub 공식 안내](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)

게시 후에는 다음과 같이 받을 수 있습니다. 비공개 패키지는 먼저 GHCR 로그인이 필요하며, 공개 배포 여부는 GitHub Packages에서 설정합니다.

```sh
docker pull ghcr.io/qmdch1/gatedock:latest
```

받은 이미지를 사용하려면 `compose.yaml`의 `image` 값을 위 주소로 바꾸고 `docker compose up -d --no-build`로 실행하세요. 게시 이미지는 Linux amd64용입니다.

클라우드 서버에서 실행할 수도 있지만, **현재 관리 화면에는 사용자 로그인 기능이 없습니다.** 서버의 로컬 주소를 SSH 포워딩 등으로 연결해 사용하고 관리 포트를 인터넷에 공개하지 마세요. 이미지를 저장소에 올리는 것만으로 웹 서비스가 배포되거나 팀 설정이 공유되지는 않습니다.

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
