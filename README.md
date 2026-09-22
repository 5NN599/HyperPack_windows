# HyperPack (Windows x64)

Open-source high-performance lossless archiver focused on large-scale streaming compression.

**Current version: v0.3.3 Xtreme+**

## Highlights

- HPK4 format with 128 MiB Level 9 dictionary
- Solid streaming and up to 8 compression workers
- 4-byte hash Match Finder with recent-first candidate search
- Deeper lazy parsing and RLE hybrid path
- Variable-length distance/length encoding
- Optional DEFLATE second-pass squeeze
- Optional OpenCL GPU hash assist with CPU fallback
- Large-file streaming and CRC integrity checks

## Current test status

User-measured development baselines include:

- 671MB `a` repeated file: HPK ~45KB, 7-Zip ~102KB, ZIP ~772KB
- ~1GB LM Studio data: HyperPack is about 5MB from ZIP in the tested run

These are dataset-specific measurements, not universal compression claims.

## Usage

Run `HyperPack.exe`, select files or a folder, choose Level 0~9, and create an `.hpk` archive.
Use `HyperPack_debug.exe` or `Run_Debug.bat` for console diagnostics.

## License

MIT License.
