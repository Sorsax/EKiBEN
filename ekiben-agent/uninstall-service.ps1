param(
  [string]$ConfigPath = (Join-Path $PSScriptRoot "agent.config.psd1"),
  [string]$ServiceName
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $ConfigPath)) {
  Write-Host "Config file not found at $ConfigPath. Falling back to parameter ServiceName."
}

if (-not $PSBoundParameters.ContainsKey("ServiceName")) {
  try {
    $cfg = Import-PowerShellDataFile $ConfigPath
    if ($cfg.ContainsKey("ServiceName")) { $ServiceName = $cfg.ServiceName }
  } catch { }
}

if (-not $ServiceName) {
  Write-Error "ServiceName not specified and not found in config. Provide -ServiceName or set ServiceName in config.";
}

Write-Host "Stopping and removing service '$ServiceName'"
try {
  sc.exe stop $ServiceName | Out-Null
} catch { }
Start-Sleep -Seconds 2
try {
  sc.exe delete $ServiceName | Out-Null
  Write-Host "Service deleted."
} catch {
  Write-Error "Failed to delete service $ServiceName: $_"
}

Write-Host "If you also want to remove binaries or config, do that manually." 
