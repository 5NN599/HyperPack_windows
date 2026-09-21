# Local Engine Sanity Benchmark

The test environment cannot reproduce the user's 1 GB LM Studio dataset, so this is a correctness/engine sanity benchmark only.

## 32 MiB highly repetitive structured data

- Raw: 33,554,432 bytes
- HPK4 token/entropy payload: 9,067 bytes
- Ratio: 0.0003
- 4 groups x 8 MiB
- CPU path
- All groups round-tripped byte-for-byte

This dataset is intentionally extremely repetitive and is not representative of general files.
