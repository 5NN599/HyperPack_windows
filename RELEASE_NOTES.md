# v0.2.8

## Critical code review fixes

- UI message loop thread is pinned with `runtime.LockOSThread()`.
- UI controls are no longer updated from the worker goroutine.
- Worker status is polled from `WM_TIMER` on the UI thread.
- Worker completion is delivered only through `PostMessageW`.
- `WNDCLASSEXW` callback and class-name storage are kept alive explicitly.
- Startup logging added to `%TEMP%\\HyperPack_startup.log`.
- Startup error messages now include the Windows error returned by the failed initialization call.
- Worker/temp path cleanup is centralized after operation completion.

## Core validation

Linux-side core extraction tests passed for:
- Levels 0..9 round-trip
- Random 8 MiB round-trip
- Multi-file package round-trip
- Archive path safety checks
- Streaming file round-trip

The release EXE was cross-compiled as PE32+ Windows x86-64 with GUI subsystem.
