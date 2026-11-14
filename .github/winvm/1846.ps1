. "$env:VM\lib.ps1"
# #1846 (#1840): text under Scale / AnimatePose / Cache is as sharp as the
# same text laid out at that size, on D3D11 (WARP on the runner, no GPU).
# The same check app is built on the PR and on main, for a before/after.
"== display"
"screen: $([System.Windows.Forms.Screen]::PrimaryScreen.Bounds)"
"dpi: $((Get-ItemProperty 'HKCU:\Control Panel\Desktop' -ErrorAction SilentlyContinue).LogPixels)"
go build -o "$env:RUNNER_TEMP\sharpness.exe" "$env:VM\sharpness"
function Run($tag, $exe, $mode, $shots) {
  $args2 = @(); if ($mode) { $args2 = $mode.Split(' ') }
  $p = StartApp $exe $args2 1
  FrontWindow $p.MainWindowHandle
  foreach ($s in $shots) {
    Start-Sleep -Milliseconds $s.wait
    $name = "$tag-$($mode -replace ' ','-')-$($s.name)".Replace('--','-')
    Shot $name | Out-Null
    "$name : $(& "$env:RUNNER_TEMP\sharpness.exe" "$env:OUT\$name.png" | Select-Object -Last 1)"
  }
  Stop-Process $p -Force -ErrorAction SilentlyContinue; Start-Sleep 1
}
function All($tag) {
  $exe = "$env:RUNNER_TEMP\textscale-$tag.exe"
  BuildCheck textscale $exe
  "== $tag ($(git rev-parse --short HEAD))"
  Run $tag $exe '' @(@{name='rest'; wait=2500})
  Run $tag $exe 'cache' @(@{name='rest'; wait=2500})
  # The pose grows 2s in, over 1.5s: mid-animation at ~2.7s, at rest from ~3.6s.
  Run $tag $exe 'anim' @(@{name='mid'; wait=1700}, @{name='rest'; wait=3000})
  Run $tag $exe 'anim cache' @(@{name='mid'; wait=1700}, @{name='rest'; wait=3000})
}
All pr
git fetch -q --depth 1 origin main
git worktree add -q "$env:RUNNER_TEMP\main" FETCH_HEAD
Push-Location "$env:RUNNER_TEMP\main"
try { All main } finally { Pop-Location }
"== counter, pr and main"
go build -o "$env:RUNNER_TEMP\counter-pr.exe" ./examples/counter-native
$p = StartApp "$env:RUNNER_TEMP\counter-pr.exe" @() 3; Shot counter-pr | Out-Null; Stop-Process $p -Force; Start-Sleep 1
Push-Location "$env:RUNNER_TEMP\main"
try { go build -o "$env:RUNNER_TEMP\counter-main.exe" ./examples/counter-native } finally { Pop-Location }
$p = StartApp "$env:RUNNER_TEMP\counter-main.exe" @() 3; Shot counter-main | Out-Null; Stop-Process $p -Force
python -m pip install -q pillow
python -c "import sys;from PIL import Image,ImageChops;a=Image.open(sys.argv[1]).convert('RGB');b=Image.open(sys.argv[2]).convert('RGB');d=ImageChops.difference(a,b).getbbox();print('counter pr vs main differ in', d)" "$env:OUT\counter-pr.png" "$env:OUT\counter-main.png" 2>&1
