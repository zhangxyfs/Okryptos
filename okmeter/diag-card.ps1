$src = @'
using System;
using System.Runtime.InteropServices;
public class WC {
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
}
'@
Add-Type -TypeDefinition $src
$h = (Get-Process OkMeter | Select-Object -First 1).MainWindowHandle
if ($h -eq 0) { Write-Host "no hwnd"; exit 1 }
# hover item2 (local 164,150) → wait → dump
[WC]::PostMessage($h, 0x0200, [IntPtr]::Zero, [IntPtr]((150 -shl 16) -bor 164)) | Out-Null
Start-Sleep -Milliseconds 1200
[WC]::PostMessage($h, 0x8000+0x4C, [IntPtr]0, [IntPtr]::Zero) | Out-Null
Start-Sleep -Milliseconds 400
Copy-Item 'C:\Users\Administrator\.okryptos\okmeter\dump-live.png' 'C:\Users\Administrator\.okryptos\okmeter\dump-card.png' -Force
Write-Host ok
