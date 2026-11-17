# Windows local-terminal ConPTY

These are the unmodified Windows binaries from Microsoft's
[`Microsoft.Windows.Console.ConPTY`](https://www.nuget.org/packages/microsoft.windows.console.conpty)
package, version **1.25.260930003**, MIT licensed (see `LICENSE.txt`).

Motty's local Windows terminal loads `conpty.dll` instead of the inbox ConPTY in
kernel32. The inbox one re-renders the child's output and consumes its color
queries, so a program's synchronized update (DECSET 2026) is painted after the
end marker and OSC 10/11 never reach the terminal parser. The package's ConPTY
forwards the child's stream instead. Terminal emulators that need the child's
stream unchanged ship the same pair for the same reason.

## Files

| File | Package path | SHA-256 |
| --- | --- | --- |
| `x64/conpty.dll` | `runtimes/win-x64/native/conpty.dll` | `feeef341d891643c62d30b6b07800bc70f0bc148f44cb8c3bee84aa557ae805a` |
| `x64/x64/OpenConsole.exe` | `build/native/runtimes/x64/OpenConsole.exe` | `3d66b23d0a71bb8eed2b77edc8b9df9bf54ce6c8fb74c863a30e760997f80586` |
| `x64/arm64/OpenConsole.exe` | `build/native/runtimes/arm64/OpenConsole.exe` | `4313825a3ba634d113cc6ed3bc8ce735e3c19d360cecd985794787479469f91f` |
| `arm64/conpty.dll` | `runtimes/win-arm64/native/conpty.dll` | `151a02355227710c20c9f8f1eaf68ac4668aa09cdc921adc7ec661804d8eb1bf` |
| `arm64/arm64/OpenConsole.exe` | `build/native/runtimes/arm64/OpenConsole.exe` | `4313825a3ba634d113cc6ed3bc8ce735e3c19d360cecd985794787479469f91f` |

The `.nupkg` itself is SHA-256
`02b07b349af66d801159bdf9e440d4a1ce78bb951f37fc8609731665afdae7ee`.

`conpty.dll` matches the program's architecture. `OpenConsole.exe` matches the
Windows kernel's architecture, so the x64 tree also carries the arm64 console
host for x64 Motty running on Windows on ARM. The layout follows the package's
MSBuild targets; `conpty.dll` looks for `x64\OpenConsole.exe`,
`arm64\OpenConsole.exe` and `x86\OpenConsole.exe` beside itself.

## Updating

Replace the files and this table together. Verify the package hash and each
file's PE machine type before copying, as the package's MSBuild targets expect;
the release packaging references these paths in `tools/release.sh`. The
installer and portable zip ship the arch tree with the program, and the portable
updater replaces the tree and `motty.exe` as one payload.
