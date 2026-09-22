# HyperPack (Windows x64)

An open-source high-performance streaming archiver designed for large-scale data payload testing.

AI와 함께 개발 중인 대용량 스트리밍 압축 프로그램, **HyperPack**의 Windows x64 데모 배포판입니다.

> ⚠️ **Notice:** 본 버전은 여러 테스트와 안정성 검증을 진행하는 과정에서 공개하는 데모(Demo) 빌드입니다. 현재 1GB 크기의 LM Studio 데이터를 기준으로 테스트 시 7z 대비 약 200MB, ZIP 대비 약 100MB 정도 압축률이 뒤처져 있으나, 안정성을 확보하고 향후 7z 이상의 성능을 달성하는 것을 목표로 지속 개선 중입니다. 다양한 환경에서의 테스트와 피드백을 부탁드립니다.

---

## 🚀 주요 기능 (Features)

- **멀티 아카이빙:** 여러 파일 및 폴더 전체를 하나의 `.hpk` 파일로 아카이빙
- **압축률 옵션:** HPK 압축 해제 및 Level 0~9 압축률 조절 지원
- **대용량 스트리밍:** 대용량 파일을 스트리밍 방식으로 처리
- **Solid 압축:** 여러 파일의 데이터를 하나의 논리적 스트림으로 처리
- **고압축 모드:** 높은 압축 레벨에서 CPU와 메모리를 적극적으로 사용
- **논블로킹 GUI:** 압축/해제 작업 중에도 GUI 응답성 유지
- **하위 호환성:** HPK1/v1 포맷 호환

## 🛠️ 핵심 수정 사항 (v0.2.8)

1. **UI 스레드 안정화:** Win32 UI 스레드를 `runtime.LockOSThread()`로 명시적으로 고정
2. **교착 상태 방지:** 백그라운드 작업과 UI 스레드의 접근을 분리하여 응답 없음 문제 개선
3. **메모리 수명 보장:** `WNDCLASSEXW` 클래스명 문자열 수명 문제 수정
4. **오류 추적 기능 추가:** 초기화 오류 발생 시 `%TEMP%\HyperPack_startup.log`에서 확인 가능

## 💻 실행 및 디버그 (Usage & Debug)

1. 기본 실행을 위해 `HyperPack.exe`를 더블클릭합니다.
2. 문제가 발생할 경우 `Run_Debug.bat`를 실행하여 콘솔 메시지를 확인해 주세요.
3. 로그 파일 위치:

```text
%TEMP%\HyperPack_startup.log
```

## 📊 현재 테스트 상태 (Benchmark)

대표 테스트 데이터는 약 1GB 크기의 LM Studio 데이터입니다.

- **7-Zip:** 기준
- **HyperPack:** 7-Zip보다 약 200MB 큼
- **ZIP:** HyperPack보다 약 100MB 작음

> 위 결과는 특정 테스트 데이터셋에서의 개발 중 측정값이며, 모든 파일에서 동일하게 나타나는 것은 아닙니다.

## 🤝 기여 및 피드백 (Contributing)

HyperPack은 오픈소스 프로젝트입니다.

대용량 파일 안정성 테스트, 압축률 개선 아이디어, 알고리즘 제안, 성능 최적화 및 코드 리팩토링을 환영합니다.

버그나 제안 사항은 **Issues** 탭에 등록해 주세요.

## 📄 라이선스 (License)

본 프로젝트는 [MIT License](LICENSE)에 따라 자유롭게 복제, 수정, 배포 및 상업적 이용이 가능합니다.
