# HPK4 Format

Header is 44 bytes and uses little-endian fields.

- magic: HPK4
- version: 1
- flags
- group size
- dictionary size
- group count
- entry count
- manifest offset
- original size
- original CRC32

Each group:

1. uncompressed size: VarInt
2. compressed size: VarInt
3. mode: 0=LZ, 1=raw, 2=DEFLATE(tokens)
4. CRC32
5. payload

LZ token stream uses one control byte per eight tokens.
Literal token: 8-bit literal.
Match token: VarInt distance + VarInt(length - 4).
