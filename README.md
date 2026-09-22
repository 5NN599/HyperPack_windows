# HyperPack v0.3.3 Xtreme+

**HyperPack**은 대규모 데이터 압축 및 고성능 무손실 압축 연구를 위해 개발된 오픈소스 아카이버(Archiver)입니다. 멀티코어 CPU 자원, 대용량 사전 메모리 및 OpenCL 기반 GPU 가속 파이프라인을 적극적으로 활용하여 높은 압축률과 가속 성능을 목표로 합니다.

> ⚠️ **Demo Notice**
> 
> HyperPack v0.3.3은 현재 알고리즘 최적화 및 테스트가 진행 중인 개발 단계의 프로젝트입니다. 데이터의 고유한 패턴에 따라 기존 7-Zip이나 standard ZIP과 압축률 차이가 발생할 수 있으며, 다양한 데이터셋을 바탕으로 안정성, 압축률, 속도를 지속적으로 개선하고 있습니다.

---

## 🚀 Key Features

* **HPK Container & Solid Streaming**: 여러 파일과 복잡한 디렉토리 구조를 단일 연속 스트림 문맥(Context)으로 패키징하여 파일 간 중복 데이터를 효과적으로 탐색 및 제거
* **128 MiB Large Dictionary**: 최대 128 MiB 슬라이딩 사전(Sliding Dictionary) 및 링 버퍼 지원을 통해 장거리(Long-Distance) 중복 패턴 탐색 능력을 극대화
* **고도화된 Match Finder & Lazy Parsing**: 
  * 4-byte Hash 기반 Recent-first candidate search 적용
  * Deeper Lazy Parsing 및 RLE Hybrid 탐색 경로 조합으로 정밀한 압축 수행
* **8-Worker Multi-Threading**: 최대 8개 Worker 스레드에 파이프라인을 효율적으로 분할 할당하여 대용량 스트림 처리 가속
* **Variable-Length Encoding**: Length 및 Distance 표기 시 가변 길이 인코딩을 적용하여 메타데이터 헤더 오버헤드 최소화
* **2-Pass Compression Pipeline**: 필요에 따라 DEFLATE 2nd pass 프로세스를 거쳐 추가적인 용량 절감 시도
* **OpenCL GPU Hash Assist & CPU Fallback**: OpenCL을 통해 GPU에서 해시 탐색을 가속하며, 미지원 환경에서는 안전하게 CPU Fallback으로 전환
* **CRC 무결성 검증 & Debugging**: 스트림 블록 단위 CRC 체크를 통한 데이터 복원 무결성 보장 및 `Run_Debug.bat` 콘솔 로깅 지원

---

## 🧪 Current Status & Goals

* **Target Version**: `v0.3.3 Xtreme+ (Windows x64)`
* **주요 개발 목표**:
  * 7-Zip 등 기존 주요 아카이버와의 압축률 격차 축소 및 고유 압축 경쟁력 확보
  * 복합 바이너리 및 대용량 데이터 환경에서의 Long-Distance Match 탐색 효율 제어
  * 소형 파일 다수 포함 시 디렉토리 스캔 및 인덱싱 성능 최적화
  * OpenCL 파이프라인 메모리 전송 구조 개편을 통한 GPU 가속 실효성 증대
* **주요 테스트 데이터**: 약 1 GB 규모의 LM Studio 실제 모델 데이터셋 및 극단적 반복 패턴 벤치마크 데이터 활용

---

## 📂 Project Structure

```text
├── HyperPack.exe             # C++ 기반 메인 압축/해제 CLI 바이너리
├── Run_Debug.bat             # 콘솔 로그 출력 및 디버깅 전용 실행 스크립트
├── BUILD_WINDOWS.bat         # Windows 환경 빌드 스크립트
├── BUILD_INFO.md             # 빌드 사양 및 바이너리 메타데이터
├── BENCHMARK_REPORT.md       # 내부 성능 분석 리포트
├── CODE_REVIEW.md            # 코드 구조 및 알고리즘 리뷰 문서
├── gui/
│   └── hyperpack_gui.py      # Tkinter 기반 GUI 프론트엔드
└── docs/
    ├── ALGORITHM_0_3_3.md    # v0.3.3 차세대 알고리즘 명세서
    ├── FORMAT_HPK4.md        # HPK4 아카이브 포맷 규격서
    └── bench_sizes.py        # 벤치마크 용량 측정 자동화 스크립트
```

---

## 💻 Usage

### 1. GUI 프론트엔드 실행
```bash
python gui/hyperpack_gui.py
```
GUI 인터페이스에서 대상 파일/폴더를 선택하고 압축 옵션(Level 0~9, 사전 크기 등)을 설정하여 `.hpk` 아카이브 생성 및 해제를 수행할 수 있습니다.

### 2. CLI 커맨드라인 사용
```cmd
:: 기본 압축 명령
HyperPack.exe c archive.hpk target_folder

:: 128 MiB Dictionary 및 8 Worker 스레드 지정 압축
HyperPack.exe c -d 128m -w 8 archive.hpk target_folder

:: 아카이브 압축 해제
HyperPack.exe x archive.hpk -o ./extracted
```

### 3. 디버그 모드 실행
작업 수행 중 상세한 동작 과정이나 로그 확인이 필요한 경우 `Run_Debug.bat`를 실행하여 콘솔 로그를 파악할 수 있습니다.

---

## 📊 Benchmark Insights

> **Note**: 본 수치는 특정 테스트 데이터셋 환경에서 측정한 실측 결과이며, 데이터의 유형 및 구성에 따라 압축 결과가 달라질 수 있습니다.

| 테스트 데이터셋 | 원본 용량 | HyperPack v0.3.3 | 7-Zip | ZIP (Standard) | 비고 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **TEST A (극단적 반복 데이터)** | 약 671 MB | **약 45 KB** | 약 102 KB | 약 772 KB | 128MB 사전 및 RLE Hybrid 탐색 작렬 |
| **TEST B (LM Studio 데이터)** | 약 1 GB | **ZIP 대비 ~5 MB 차이** | - | 기준점 | 일반 복합 바이너리 스트림 환경 |

---

## 🤝 Contributing

HyperPack은 오픈소스 프로젝트입니다. 다음과 같은 형태의 기여를 언제나 환영합니다:
* 다양한 실전 데이터셋 기반 압축률 및 속도 벤치마크 리포트 제출
* 대용량 파일 및 소형 파일 다수 포함 시의 안정성 검증
* Match Finder, Parsing 알고리즘 및 OpenCL/CPU 멀티스레드 최적화 아이디어 제안
* 버그 리포트 및 문서/주석 개선

---

## 📄 License

본 프로젝트는 **[MIT License](LICENSE)** 하에 자유롭게 이용, 수정 및 배포할 수 있습니다.
