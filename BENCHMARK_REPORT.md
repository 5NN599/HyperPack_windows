# v0.3.3 Benchmark / Validation

## User-measured real-world baselines

| Dataset | HyperPack | 7-Zip | ZIP |
|---|---:|---:|---:|
| 671MB `a` repeated file | ~45KB | ~102KB | ~772KB |
| ~1GB LM Studio data | ZIP보다 약 5MB 차이 | - | 기준 |

These are development measurements supplied from the user's Windows test environment.
They are dataset-specific and are not a universal compression claim.

## Core engine comparison

A Linux-hosted portable harness was used only to compare the HPK4 encoder/decoder core,
with OpenCL disabled and the same synthetic inputs for both versions.

| Test | v0.3.2 core | v0.3.3 core |
|---|---:|---:|
| 8MiB `a` repeated | 532 B | 532 B |
| 8MiB 7-byte motif | 593 B | 538 B |
| 8MiB structured pattern | 901 B | 421 B |

All three round-tripped byte-for-byte in the portable core harness.

The v0.3.3 comparison is intentionally used as an engineering regression check;
these synthetic sizes are not representative of general files.

## Build validation

- Windows x64 PE release build: PASS
- Windows x64 debug build: PASS
- Core CRC combine validation: PASS
- Portable HPK4 round-trip validation: PASS
- Windows GUI runtime: requires a Windows desktop test environment
