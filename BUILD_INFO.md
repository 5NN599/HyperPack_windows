# Build Info

Target: Windows x64
Build: Go cross-compiled PE32+
GUI: windowsgui subsystem
Debug build: console subsystem

HyperPack.exe SHA-256:
279c8c34510014185b8ad8b0274a59312cafcbe400f9c162456e7d8d3e891f93

HyperPack_debug.exe SHA-256:
6a3d8e25bd8a1550e8be347c77f93363b99eeb315a8485025843bb3f365ffce0

Core sanity test in the build environment:
- repetitive data round-trip: PASS
- structured patterned data round-trip: PASS
- DEFLATE squeeze path: PASS
- Windows PE format: PASS

Note: the provided environment cannot execute a Windows desktop binary, so final GUI runtime validation must be performed on Windows.
