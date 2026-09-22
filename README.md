**HyperPack** is an open-source high-performance lossless archiver focused on
large-scale data compression and resource-intensive compression testing.

현재 **v0.3.2 Xtreme+** 개발 버전으로, 높은 압축률을 목표로 CPU, 메모리 및
지원되는 경우 GPU를 적극적으로 활용하도록 개발하고 있습니다.

> ⚠️ **Demo Notice:** HyperPack은 현재 개발 및 테스트 단계입니다.
> 실제 데이터에서는 7-Zip/ZIP보다 압축 결과가 클 수 있으며, 다양한 데이터셋에서
> 안정성·압축률·속도를 지속적으로 개선하고 있습니다.

---

## 🚀 Features

- **HPK Archive:** 여러 파일과 폴더를 하나의 `.hpk`로 압축
- **Level 0~9:** 압축률과 처리량을 조절
- **Solid Streaming:** 여러 파일의 중복 데이터를 함께 탐색
- **Large Dictionary:** 최대 128 MiB Dictionary 사용
- **Multi-Worker:** 최대 8개 Worker를 활용한 병렬 처리
- **Variable-Length Encoding:** 압축 데이터의 메타데이터 오버헤드 감소
- **2-Pass Compression:** 추가 압축 단계로 압축률 개선 시도
- **GPU Hash Assist:** OpenCL 기반 GPU 가속 실험 기능
- **CPU Fallback:** GPU를 사용할 수 없는 환경에서도 CPU로 동작
- **Large File Streaming:** 대용량 파일을 스트리밍 방식으로 처리

## 🧪 Current Status

**Version: v0.3.2 Xtreme+**

현재 주요 개발 목표:

- 7-Zip과의 압축률 격차 축소 및 경쟁
- 대용량 데이터에서 Long-Distance Match 개선
- Match Finder 및 Parsing 최적화
- 멀티코어 CPU 활용 개선
- GPU 가속 실험 및 최적화
- 압축/해제 안정성 개선

대표 테스트 데이터로 약 **1GB LM Studio 데이터** 등을 사용하고 있습니다.

> 테스트 결과는 데이터 종류와 압축 설정에 따라 크게 달라질 수 있습니다.

## 💻 Usage

1. `HyperPack.exe`를 실행합니다.
2. 압축할 파일 또는 폴더를 선택합니다.
3. 압축 레벨을 선택합니다.
4. `.hpk` 파일을 생성합니다.
5. 필요할 경우 HyperPack으로 압축을 해제합니다.

문제 발생 시 함께 제공되는 `Run_Debug.bat`를 사용하여 콘솔 로그를 확인할 수
있습니다.

## 🤝 Contributing

HyperPack은 오픈소스 프로젝트입니다.

압축률 테스트, 대용량 파일 안정성 테스트, 알고리즘 개선, 성능 최적화 및
버그 리포트를 환영합니다.

## 📄 License

This project is released under the **MIT License**.
"""
