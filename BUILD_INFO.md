# Build Info

Version: v0.3.3 Xtreme+
Target: Windows x64
Core: Go HPK4 engine
GUI: Win32 GUI subsystem
Debug build: console subsystem

## SHA-256

HyperPack.exe:
`45f43a25a0845d3b9027c40ccb04f89c2def5ab90cdaff7eac24300bec4a17fe`

HyperPack_debug.exe:
`6ef1d35c9ec52ca8044ab36c3149991c1ec215f496ebd28b45101e9443fe8c96`

## Validation

- Windows x64 PE release build: PASS
- Windows x64 PE debug build: PASS
- CRC32 combine helper: PASS
- HPK4 core round-trip harness: PASS
- v0.3.2 vs v0.3.3 core regression comparison: PASS

The build environment here cannot execute the Windows desktop GUI, so final Windows GUI
runtime testing should still be performed on a Windows machine.
