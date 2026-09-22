# v0.3.3 Critical Code Review

## v0.3.2에서 확인된 병목

- 8-byte hash가 최소 match 길이 4보다 강한 조건이라 4~7 byte match를 놓칠 수 있음
- candidate ring이 오래된 슬롯부터 순회하여 최근 후보의 이점을 충분히 활용하지 못함
- Level 8/9 lookahead가 1-byte에 머묾
- 압축 시작 전에 전체 입력 CRC를 별도로 스캔함
- worker 메모리 예산이 hash index 실제 크기를 충분히 반영하지 않음

## v0.3.3 수정

1. 4-byte hash + 20-bit / 16-slot 구조로 변경
2. recent-first candidate search 적용
3. 최대 4-position lazy lookahead 추가
4. distance=1 RLE hybrid 경로 추가
5. per-group CRC combine으로 전체 CRC 선행 스캔 제거
6. worker memory budget에 hash index 약 64 MiB 반영

## 남은 과제

- full optimal parsing
- context-adaptive entropy coding / ANS / range coder
- OpenCL context/program 재사용 최적화
- 수만 개 파일을 포함한 36GB급 package 생성 단계의 디스크 병목 개선
- 동일 원본에 대한 Windows 실환경 7-Zip/ZIP/HyperPack 자동 벤치마크
