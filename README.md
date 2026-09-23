# HyperPack v0.3.3 Xtreme+ (Windows x64)

**HyperPack** is an open-source high-performance lossless archiver focused on large-scale streaming compression and experimental high-ratio compression techniques.

현재 배포 버전은 **v0.3.3 Xtreme+**이며, HPK4 포맷을 기반으로 대용량 파일과 대규모 데이터 집합에서 높은 압축률과 안정적인 복원을 목표로 개발하고 있습니다.

> ⚠️ **Development Release:** HyperPack은 현재 개발 및 테스트 단계입니다. 아래 벤치마크는 특정 데이터셋과 설정에서 측정한 결과이며 모든 파일에 동일하게 적용되는 성능을 의미하지 않습니다.

---

## 🚀 주요 기능

- **HPK4 Archive Format** — 자체 무손실 압축/아카이빙 포맷
- **128 MiB Dictionary** — 장거리 중복 패턴 탐색
- **Solid Streaming** — 여러 파일의 데이터를 연속 스트림으로 처리
- **최대 8 Workers** — 멀티코어 CPU 병렬 압축
- **4-byte Hash Match Finder** — 짧은 매치와 중간 길이 반복 패턴 탐색 개선
- **Recent-first Search** — 최근 후보를 우선 탐색
- **Deeper Lazy Parsing** — 높은 압축 레벨에서 더 많은 후보를 비교
- **RLE Hybrid Path** — 긴 동일 바이트 반복 구간 처리 개선
- **Variable-Length Encoding** — Length / Distance 저장 오버헤드 감소
- **선택적 DEFLATE 2차 압축** — 추가 압축 단계 지원
- **OpenCL GPU Hash Assist** — 실험적 GPU 가속 경로
- **CPU Fallback** — GPU를 사용할 수 없는 환경에서도 CPU로 동작
- **Streaming 처리** — 대용량 파일을 스트리밍 방식으로 처리
- **CRC 무결성 검증** — 압축 데이터의 무손실 복원 검증

---

## 🛠️ v0.3.3 주요 변경 사항

### Match Finder 개선

기존 8-byte hash 중심의 검색에서 **4-byte hash 기반 검색**을 추가하여 4~7 byte 수준의 짧은 반복 패턴도 더 적극적으로 탐색하도록 개선했습니다.

### Recent-first Candidate Search

후보 검색 순서를 최근 슬롯부터 확인하도록 변경하여 최근에 발견된 유효한 매치를 더 빠르게 선택할 수 있도록 했습니다.

### Deeper Lazy Parsing

Level 8/9에서 최대 4개 위치까지 추가 lookahead를 수행하여 즉시 선택하는 것보다 더 유리한 매치 조합을 찾을 수 있도록 개선했습니다.

### RLE Hybrid

긴 동일 바이트 반복 구간에서 **distance=1 match**를 활용하는 하이브리드 경로를 추가했습니다. 기존 HPK4 구조를 유지하면서 반복 데이터 처리 효율을 높이는 방향입니다.

### 대용량 CRC 처리 개선

압축 시작 전에 전체 입력을 다시 스캔하던 방식을 개선하고, **per-group CRC combine** 방식으로 최종 CRC를 계산하여 불필요한 대용량 입력 선행 스캔을 줄였습니다.

### 메모리 예산 개선

Worker 메모리 계산에 실제 hash index 사용량을 반영하여 대형 Dictionary와 병렬 작업을 함께 사용할 때의 메모리 계획을 개선했습니다.

### HPK4 호환성

이번 버전의 주요 변경은 **encoder 중심의 개선**이며 기존 HPK4 decoder와의 호환성을 유지하도록 설계했습니다.

---

## 📊 실제 테스트 결과

### 671MB `a` 반복 데이터

| Compressor | Compressed Size |
|---|---:|
| **HyperPack** | **약 45 KB** |
| 7-Zip | 약 102 KB |
| ZIP | 약 772 KB |

매우 높은 반복성을 가진 데이터에서 HyperPack의 장거리 매칭 및 스트리밍 압축 구조가 특히 강하게 나타난 사례입니다.

### 약 1GB LM Studio 데이터

사용자 Windows 환경의 실측에서 HyperPack과 ZIP의 최종 압축 크기 차이는 **약 5MB** 수준이었습니다.

> 두 결과 모두 특정 데이터셋에서의 개발용 실측값이며, 일반적인 모든 데이터에서 동일한 압축률을 보장하는 수치는 아닙니다.

---

## 🧪 Core Regression Test

v0.3.2와 v0.3.3의 HPK4 core를 동일한 합성 입력으로 비교했습니다.

| Test | v0.3.2 | v0.3.3 |
|---|---:|---:|
| 8 MiB `a` repeated | 532 B | 532 B |
| 8 MiB 7-byte motif | 593 B | 538 B |
| 8 MiB structured pattern | 901 B | 421 B |

세 테스트 모두 압축 후 압축 해제한 데이터가 원본과 byte-for-byte 일치함을 확인했습니다.

이 표는 **엔지니어링 회귀 테스트**이며 실제 일반 파일의 압축 성능을 대표하는 벤치마크는 아닙니다.

---

## 💻 실행 방법

일반 실행:

```text
HyperPack.exe
```

진단용 실행:

```text
HyperPack_debug.exe
Run_Debug.bat
```

일반적인 사용 순서:

1. `HyperPack.exe` 실행
2. 압축할 파일 또는 폴더 선택
3. 압축 레벨(Level 0~9) 선택
4. `.hpk` 아카이브 생성
5. 필요할 경우 HyperPack으로 압축 해제

---

## ⚠️ 현재 알려진 제한 및 개발 과제

현재 다음 영역은 계속 개선 중입니다.

- Full optimal parsing
- Context-adaptive entropy coding / ANS / Range Coder
- OpenCL context/program 재사용 최적화
- 수만 개 파일을 포함하는 36GB급 패키지 생성 단계의 디스크/인덱싱 병목
- Windows 실환경에서의 자동 ZIP / 7-Zip / HyperPack 벤치마크
- 일반적인 비반복성 데이터의 압축률 개선

---

## 🔐 검증 정보

이번 v0.3.3 배포판에서는 다음 검증을 수행했습니다.

- Windows x64 PE release build: **PASS**
- Windows x64 PE debug build: **PASS**
- CRC32 combine validation: **PASS**
- HPK4 core round-trip validation: **PASS**
- v0.3.2 → v0.3.3 core regression comparison: **PASS**

Windows GUI 자체 실행은 빌드 환경에서 수행할 수 없으므로 최종 GUI 런타임 검증은 실제 Windows 시스템에서 추가 확인이 필요합니다.

---

## 📄 License

HyperPack is released under the **MIT License**.
