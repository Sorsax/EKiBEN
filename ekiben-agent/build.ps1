param(
  [string]$Icon = "icon.ico"
)

$ErrorActionPreference = "Stop"

$root = Resolve-Path (Split-Path -Parent $PSCommandPath)
$iconPath = Join-Path $root $Icon

if (Test-Path $iconPath) {
  Write-Host "Generating icon resources..."
  $sysoPath = Join-Path $root "cmd\agent\app.syso"
  $arch = $env:GOARCH
  if (-not $arch) {
    $arch = (go env GOARCH)
  }
  Get-ChildItem -Path (Join-Path $root "cmd\agent") -Filter "*.syso" -ErrorAction SilentlyContinue | Remove-Item -Force
  go run github.com/tc-hib/go-winres@latest simply --icon $iconPath --manifest cli --out $sysoPath --arch $arch
  if (-not (Test-Path $sysoPath) -and -not (Test-Path ("$sysoPath`_windows_$arch.syso"))) {
    Write-Error "Icon resource generation failed: no .syso was created for $arch"
  }
}

Write-Host "Building ekiben-agent.exe..."

go build -o ekiben-agent.exe ./cmd/agent
