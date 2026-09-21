# v0.3.2 Critical Code Review

## v0.3.1의 문제

- 큰 dictionary라는 설명과 달리 실제 토큰 매처의 window 사용이 제한적이었습니다.
- 고정 3-byte match metadata에 비해 VarInt 이점이 충분히 반영되지 않았습니다.
- 압축 워커가 장거리 후보를 충분히 많이 비교하지 않았습니다.
- entropy stage가 없어 token metadata의 남은 중복을 더 줄이지 못했습니다.
- GPU 경로가 없었습니다.

## v0.3.2의 수정

- HPK4로 새 포맷 분리
- 실제 128 MiB dictionary 사용
- bucket당 32 후보
- 최대 65535-byte match
- VarInt distance/length
- lazy lookahead
- DEFLATE 2차 압축
- raw fallback
- 멀티 worker + streaming
- OpenCL GPU hash assist를 선택 기능으로 추가

## 남은 과제

- 최적 파싱(optimal parsing) 부재
- context-adaptive arithmetic/range coder 없음
- GPU는 현재 hash precomputation 보조에 한정
- 7z/LZMA2와의 대규모 실측 벤치마크가 아직 필요
