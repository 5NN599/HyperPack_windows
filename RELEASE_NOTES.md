# HyperPack v0.3.3 Release Notes

## 핵심 개선

- 4-byte hash 기반 Match Finder로 4~7 byte 짧은 반복 패턴 탐색 개선
- 최근 후보를 우선 검색하도록 hash-ring probe 순서 개선
- Level 8/9 deeper lazy parsing 추가
- 장거리 동일 바이트 구간에 거리 1 match를 사용하는 RLE hybrid 경로 추가
- HPK4 최종 CRC를 group CRC 결합으로 계산하여 대용량 입력의 전체 CRC 선행 스캔 제거
- worker memory budget에 실제 hash index 메모리를 반영
- 128 MiB dictionary / 최대 65535-byte match / 최대 8 workers 구조 유지
- 기존 HPK4 decoder와 호환되는 방식으로 encoder만 개선

## 검증

개발 환경에서 Windows x64 PE 빌드와 코어 round-trip 테스트를 수행했습니다.
Windows GUI 자체 실행 검증은 Windows 환경에서 추가 확인이 필요합니다.

사용자 실측 기준:

- 671MB `a` 반복 데이터: HPK 약 45KB / 7-Zip 약 102KB / ZIP 약 772KB
- 약 1GB LM Studio 데이터: HyperPack과 ZIP의 최종 크기 차이가 약 5MB

위 두 수치는 동일한 데이터셋에서의 개발용 실측값이며 모든 데이터에 일반화할 수 없습니다.
