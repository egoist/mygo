. "$env:VM\lib.ps1"
# #1847 (#1841): with 2000 translucent StrokePath links under a changing
# zoom, every link draws (paths the atlas has no room for spill to the
# CPU into a layer shown as an image) on D3D11 (WARP, no GPU), the spill
# is logged once, and frames keep coming. The same app is built on the
# PR and on main, for a before/after.
"== display"
"screen: $([System.Windows.Forms.Screen]::PrimaryScreen.Bounds)"
python -m pip install -q pillow
function Links($tag, $mode) {
  $exe = "$env:RUNNER_TEMP\links-$tag-$mode.exe"
  BuildCheck links $exe
  $p = StartApp $exe @($mode) 6
  FrontWindow $p.MainWindowHandle
  Start-Sleep 6
  $name = "$tag-$mode"
  Shot $name | Out-Null
  ShotRect "$name-zoom" 100 150 200 150 4 | Out-Null
  Start-Sleep 2
  Stop-Process $p -Force -ErrorAction SilentlyContinue; Start-Sleep 1
  $base = [IO.Path]::GetFileNameWithoutExtension($exe)
  "-- $name stdout (fps):"; Get-Content "$env:OUT\$base-stdout.txt" -ErrorAction SilentlyContinue
  "-- $name stderr:"; Get-Content "$env:OUT\$base-stderr.txt" -ErrorAction SilentlyContinue
  # Link pixels: bluish ones (b - r > 25), not the gray nodes or background.
  python -c "import sys;from PIL import Image;im=Image.open(sys.argv[1]).convert('RGB');print('$name link pixels', sum(1 for r,g,b in im.getdata() if b-r>25))" "$env:OUT\$name.png"
}
"== pr ($(git rev-parse --short HEAD))"
Links pr still
Links pr anim
git fetch -q --depth 1 origin main
git worktree add -q "$env:RUNNER_TEMP\main" FETCH_HEAD
Push-Location "$env:RUNNER_TEMP\main"
try {
  "== main ($(git rev-parse --short HEAD))"
  Links main still
  Links main anim
} finally { Pop-Location }
