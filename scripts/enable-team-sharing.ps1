#Requires -RunAsAdministrator
$ErrorActionPreference = 'Stop'
$ruleName = 'SSHDesk-Team-Downloads-9877'
$existing = Get-NetFirewallRule -Name $ruleName -ErrorAction SilentlyContinue
if ($existing) { Remove-NetFirewallRule -Name $ruleName }
New-NetFirewallRule -Name $ruleName -DisplayName 'SSHDesk Team Downloads (Local Subnet)' -Direction Inbound -Action Allow -Protocol TCP -LocalPort 9877 -RemoteAddress LocalSubnet -Profile Domain,Private,Public | Out-Null
Write-Output 'SSHDesk: TCP 9877 allowed from the local subnet. Management port 9876 remains private.'
