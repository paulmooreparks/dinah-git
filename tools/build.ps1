param([string]$SigningKey, [string]$Out = (Join-Path $PSScriptRoot '../dist/extension'))
$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$Out = [IO.Path]::GetFullPath($Out)
New-Item -ItemType Directory -Force (Join-Path $Out 'bin'), (Join-Path $Out 'web') | Out-Null
$previousOS = $env:GOOS
$previousArch = $env:GOARCH
$previousCGO = $env:CGO_ENABLED
try {
  $env:GOARCH = 'amd64'
  $env:CGO_ENABLED = '0'
  foreach ($target in @(@('windows','dinah-git.exe'), @('linux','dinah-git'), @('darwin','dinah-git-darwin'))) {
    $env:GOOS = $target[0]
    & go -C $repo build -trimpath -o (Join-Path $Out ('bin/' + $target[1])) ./cmd/dinah-git
    if ($LASTEXITCODE) { throw "Build failed for $($target[0])" }
  }
} finally {
  $env:GOOS = $previousOS
  $env:GOARCH = $previousArch
  $env:CGO_ENABLED = $previousCGO
}
Copy-Item -LiteralPath (Join-Path $repo 'web/index.html'), (Join-Path $repo 'web/git.js'), (Join-Path $repo 'web/git.css') -Destination (Join-Path $Out 'web') -Force
Copy-Item -LiteralPath (Join-Path $repo 'LICENSE') -Destination $Out -Force
$notices = [Collections.Generic.List[string]]::new()
$notices.Add('Third-party licenses for the statically linked Git extension backends.')
$goRoot = & go env GOROOT
$notices.Add("`nGo runtime and standard library`n")
$notices.Add([IO.File]::ReadAllText((Join-Path $goRoot 'LICENSE')))
$modules = & go -C $repo list -m '-f={{.Path}}|{{.Version}}|{{.Dir}}' all
if ($LASTEXITCODE) { throw 'Module inventory failed' }
foreach ($module in $modules) {
  $parts = $module -split '\|', 3
  if ($parts[0] -eq 'github.com/paulmooreparks/dinah-git' -or -not $parts[2]) { continue }
  $found = $false
  foreach ($name in @('LICENSE','LICENSE.md','LICENSE.txt','COPYING','NOTICE')) {
    $licensePath = Join-Path $parts[2] $name
    if (Test-Path -LiteralPath $licensePath -PathType Leaf) {
      $notices.Add("`n$($parts[0]) $($parts[1]) ($name)`n")
      $notices.Add([IO.File]::ReadAllText($licensePath))
      $found = $true
    }
  }
  if (-not $found) { throw "No license file found for $($parts[0])" }
}
[IO.File]::WriteAllText((Join-Path $Out 'THIRD-PARTY-NOTICES.txt'), ($notices -join "`n"), [Text.UTF8Encoding]::new($false))
$manifest = & go -C $repo run ./cmd/dinah-git --manifest
if ($LASTEXITCODE) { throw 'Manifest generation failed' }
[IO.File]::WriteAllText((Join-Path $Out 'dinah-extension.json'), ($manifest -join "`n"), [Text.UTF8Encoding]::new($false))
if ($SigningKey) {
  & go -C $repo run github.com/paulmooreparks/dinah-extension-go/cmd/dinah-extension sign $Out $SigningKey
  if ($LASTEXITCODE) { throw 'Signing failed' }
}
Write-Output $Out
