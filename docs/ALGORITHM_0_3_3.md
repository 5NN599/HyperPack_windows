# HyperPack 0.3.3 Algorithm

## Pipeline

Input -> solid package stream -> 8 MiB group -> 128 MiB dictionary
-> 4-byte hash match finder -> recent-first candidate probe
-> RLE hybrid -> deeper lazy parsing -> VarInt token stream
-> optional DEFLATE squeeze -> HPK4 block

## Match Finder

- hash: 4-byte source pattern
- hash table: 20-bit / 1,048,576 buckets
- slots: 16 per bucket
- total slot storage remains about 64 MiB per worker
- Level 9: up to 16 candidates per position
- maximum match: 65,535 bytes
- dictionary: 128 MiB at Level 9
- recent candidates are checked first

The 4-byte hash is intentional because the previous 8-byte key could not discover
valid matches whose length was only 4~7 bytes.

## Parsing

Level 8/9 uses up to four positions of lazy lookahead. A current match is skipped
when a later match is sufficiently longer to justify the intervening literals.

Long same-byte runs are encoded through an ordinary distance=1 match, so no new
HPK4 token type is required.

## CRC

Each group already has a CRC32. v0.3.3 combines these CRCs in output order instead
of scanning the complete input once before compression. This removes a full extra
read of very large single-file inputs.
