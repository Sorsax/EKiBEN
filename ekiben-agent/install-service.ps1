param(
  [string]$ConfigPath = (Join-Path $PSScriptRoot "agent.config.psd1"),
  [string]$ServiceName,
  [string]$Controller,
  [string]$Token,
  [string]$AgentId,
  [string]$DbPath,
  [bool]$AllowWrite,
  [bool]$LogTraffic,
  [string]$Ping,
  [string]$Reconnect,
  [string]$Timeout
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $ConfigPath)) {
  Write-Error "Config file not found at $ConfigPath. Copy agent.config.psd1 and edit it."
}

$config = Import-PowerShellDataFile $ConfigPath

if (-not $PSBoundParameters.ContainsKey("ServiceName") -and $config.ContainsKey("ServiceName")) { $ServiceName = $config.ServiceName }
if (-not $PSBoundParameters.ContainsKey("Controller") -and $config.ContainsKey("Controller")) { $Controller = $config.Controller }
if (-not $PSBoundParameters.ContainsKey("Token") -and $config.ContainsKey("Token")) { $Token = $config.Token }
if (-not $PSBoundParameters.ContainsKey("AgentId") -and $config.ContainsKey("AgentId")) { $AgentId = $config.AgentId }
if (-not $PSBoundParameters.ContainsKey("DbPath") -and $config.ContainsKey("DbPath")) { $DbPath = $config.DbPath }
if (-not $PSBoundParameters.ContainsKey("AllowWrite") -and $config.ContainsKey("AllowWrite")) { $AllowWrite = [bool]$config.AllowWrite }
if (-not $PSBoundParameters.ContainsKey("LogTraffic") -and $config.ContainsKey("LogTraffic")) { $LogTraffic = [bool]$config.LogTraffic }
if (-not $PSBoundParameters.ContainsKey("ServiceName") -and $config.ContainsKey("ServiceName")) { $ServiceName = $config.ServiceName }
if (-not $PSBoundParameters.ContainsKey("Ping") -and $config.ContainsKey("Ping")) { $Ping = $config.Ping }
if (-not $PSBoundParameters.ContainsKey("Reconnect") -and $config.ContainsKey("Reconnect")) { $Reconnect = $config.Reconnect }
if (-not $PSBoundParameters.ContainsKey("Timeout") -and $config.ContainsKey("Timeout")) { $Timeout = $config.Timeout }

$required = @("ServiceName", "Controller", "Token", "AgentId", "DbPath", "Ping", "Reconnect", "Timeout")
foreach ($name in $required) {
  if (-not (Get-Variable $name -ValueOnly)) {
    Write-Error "Missing required setting: $name"
  }
}

$exe = Join-Path $PSScriptRoot "ekiben-agent.exe"
if (-not (Test-Path $exe)) {
  Write-Error "ekiben-agent.exe not found at $exe"
}

$args = @(
  "--controller", $Controller,
  "--token", $Token,
  "--agent-id", $AgentId,
  "--db", $DbPath,
  "--ping", $Ping,
  "--reconnect", $Reconnect,
  "--timeout", $Timeout,
  "--service", $ServiceName,
  "--update-repo", $config.UpdateRepo,
  "--update-asset", $config.UpdateAsset
)

if ($AllowWrite) {
  $args += "--allow-write"
}

if ($LogTraffic) {
  $args += "--log-traffic"
}

$binPath = '"' + $exe + '" ' + ($args -join ' ')

Write-Host "Preparing service '$ServiceName' with:"
Write-Host $binPath

# If the service already exists, stop it and update its binPath; otherwise create it.
$svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($null -ne $svc) {
  Write-Host "Service '$ServiceName' exists; updating configuration."
  if ($svc.Status -ne 'Stopped') {
    Write-Host "Stopping service..."
    try {
      Stop-Service -Name $ServiceName -Force -ErrorAction Stop
    } catch {
      Write-Host "Stop-Service failed, attempting sc.exe stop"
      sc.exe stop $ServiceName | Out-Null
    }
    Start-Sleep -Seconds 2
  }

  Write-Host "Updating service binary path and start mode..."
  sc.exe config $ServiceName binPath= $binPath start= auto | Out-Null
  sc.exe description $ServiceName "Outbound WebSocket agent for EKiBEN SQLite access" | Out-Null

  Write-Host "Service updated. Start it with:"
  Write-Host "  sc.exe start $ServiceName"
} else {
  Write-Host "Service not found; creating new service."
  sc.exe create $ServiceName binPath= $binPath start= auto | Out-Null
  sc.exe description $ServiceName "Outbound WebSocket agent for EKiBEN SQLite access" | Out-Null

  Write-Host "Service installed. Start it with:"
  Write-Host "  sc.exe start $ServiceName"
}

Write-Host "To uninstall later, run uninstall-service.ps1 or:"
Write-Host "  sc.exe stop $ServiceName"
Write-Host "  sc.exe delete $ServiceName"
