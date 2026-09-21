# Critical code review notes

## 1. Cross-thread UI access

Old path: worker-status goroutine called `setProgressText()`, which called Win32 UI functions directly.

New path: worker writes a small status file; the GUI's own message thread polls it via `WM_TIMER` and updates controls itself.

## 2. Message-loop thread affinity

Old path: the GUI function did not explicitly pin its Win32 message loop to one OS thread.

New path: `runtime.LockOSThread()` surrounds the entire GUI lifetime.

## 3. Callback lifetime

The window procedure callback and class-name UTF-16 buffer are now held in package-level variables so they remain live for the Windows window-class lifetime.

## 4. Startup diagnostics

Every major startup stage is recorded in `%TEMP%\\HyperPack_startup.log`.

## 5. Existing core issues retained for a future compression-engine milestone

- CLI single-file paths still have a separate in-memory path and should be unified with the streaming path.
- The current LZ-style parser is deliberately much simpler than LZMA2/7z and is not expected to beat 7-Zip on arbitrary data yet.
- Archive extraction should eventually reject case-colliding paths (`A.txt` vs `a.txt`) before writing.
- Package metadata is currently stored in a temporary uncompressed package stream before final HPK compression; a future format can stream metadata and file blocks directly.
