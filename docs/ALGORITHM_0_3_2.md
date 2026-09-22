# HyperPack 0.3.2 Algorithm

## Pipeline

Input -> solid package stream -> 8 MiB group -> 128 MiB dictionary -> hash-ring match finder -> lazy parsing -> VarInt token stream -> optional DEFLATE squeeze -> HPK4 block

## Level 9

- dictionary: 128 MiB
- group: 8 MiB
- hash bits: 19
- candidates: 32 / bucket
- max match: 65535
- second-pass DEFLATE: BestCompression when smaller
- up to 8 concurrent workers, bounded by a conservative 4 GiB working-set budget

## GPU assist

An optional OpenCL kernel computes the 8-byte hash key for each byte position of sufficiently large groups. CPU still performs the actual match search and token decision.

This keeps the archive format vendor-neutral and makes GPU acceleration opportunistic rather than mandatory.
