param(
  [string]$Output = "ekiben-agent.zip",
  [string]$AgentDir = "ekiben-agent"
)

$ErrorActionPreference = "Stop"

$root = Resolve-Path (Split-Path -Parent $PSCommandPath)
$agentPath = Join-Path $root $AgentDir

if (-not (Test-Path $agentPath)) {
  Write-Error "Agent folder not found: $agentPath"
}

Write-Host "Building ekiben-agent.exe..."
Push-Location $agentPath

go build -o ekiben-agent.exe ./cmd/agent

Pop-Location

$tmpDir = Join-Path $root "_release_tmp"
if (Test-Path $tmpDir) {
  Remove-Item -Recurse -Force $tmpDir
}
New-Item -ItemType Directory -Path $tmpDir | Out-Null

$files = @(
  "ekiben-agent.exe",
  "agent.config.psd1",
  "install-service.ps1",
  "uninstall-service.ps1",
  "start.bat"
)

foreach ($file in $files) {
  $src = Join-Path $agentPath $file
  if (-not (Test-Path $src)) {
    Write-Error "Missing required file: $src"
  }
  Copy-Item $src (Join-Path $tmpDir $file) -Force
}

$zipPath = Join-Path $root $Output
if (Test-Path $zipPath) {
  Remove-Item $zipPath -Force
}

Compress-Archive -Path (Join-Path $tmpDir '*') -DestinationPath $zipPath

Remove-Item -Recurse -Force $tmpDir

Write-Host "Created release archive: $zipPath"
Write-Host "Note: The zip contains files at the root (no subfolder)."
