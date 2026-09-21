# v0.3.2 Release Notes

## 압축 엔진

1. HPK4 새 포맷
2. Level 9 dictionary 128 MiB
3. 8 MiB groups
4. 최대 match 65535 bytes
5. 19-bit hash table / 32 candidates per bucket
6. VarInt distance/length
7. lazy lookahead
8. DEFLATE BestCompression 2차 entropy squeeze
9. raw fallback
10. 멀티 worker 압축
11. 대용량 streaming
12. 선택적 OpenCL GPU hash assist

## 안정성

GPU 함수는 예외가 발생해도 CPU fallback으로 돌아가도록 방어했습니다.
GUI는 실제 압축 작업과 별도 프로세스에서 동작합니다.
