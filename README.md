# HyperPack (Windows x64)

An open-source high-performance streaming archiver designed for large-scale data payload testing. 
AI와 함께 개발 중인 대용량 스트리밍 압축 프로그램, **HyperPack**의 Windows x64 데모 배포판입니다.

> ⚠️ **Notice:** 본 버전은 여러 테스트와 안정성 검증을 진행하는 과정에서 공개하는 데모(Demo) 빌드입니다. 현재 1GB 크기의 LM 스튜디오 데이터를 기준으로 테스트 시 7z 대비 약 200MB, ZIP 대비 약 100MB 정도 압축률이 뒤처져 있으나, 안정성을 확보하고 향후 7z 이상의 성능을 달성하는 것을 목표로 지속 개선 중입니다. 다양한 환경에서의 테스트와 피드백을 부탁드립니다.

---

## 🚀 주요 기능 (Features)
- **멀티 아카이빙:** 여러 파일 및 폴더 전체를 하나의 `.hpk` 파일로 아카이빙
- **압축률 옵션:** HPK 압축 해제 및 Level 0~9 압축률 조절 지원
- **논블로킹 GUI:** 대용량 스트리밍 압축/해제 작업을 별도 프로세스로 분리하여 작업 중에도 GUI 반응성 유지
- **하위 호환성:** HPK1/v1 포맷 호환

## 🛠️ 핵심 수정 사항 (v0.2.8)
이전 버전(v0.2.7)에서 발견된 구조적 문제를 검토하여 안정성을 대폭 개선했습니다.
1. **UI 스레드 안정화:** Win32 UI 메시지 루프 스레드가 명시적으로 고정되지 않던 문제를 `runtime.LockOSThread()`를 통해 해결했습니다.
2. **교착 상태(Deadlock) 방지:** 백그라운드 goroutine이 UI 컨트롤(`SetWindowText`, `InvalidateRect`)을 직접 호출하여 발생하던 Windows GUI 스레드 충돌 및 응답 없음 증상을 해결했습니다. 작업 프로세스는 결과 파일 작성 후 `PostMessageW`로 UI에 완료 이벤트만 전달하며, 상태는 UI의 `WM_TIMER`가 읽도록 구조를 분리했습니다.
3. **메모리 수명 보장:** `WNDCLASSEXW`의 클래스명 문자열 수명을 명확하게 보장하도록 수정했습니다.
4. **오류 추적 기능 추가:** 시작 경로에 오류 추적 로그가 없어 초기화 실패 원인을 알 수 없던 문제를 개선했습니다. 문제 발생 시 `%TEMP%\HyperPack_startup.log`에서 세부 내용을 확인할 수 있습니다.

## 💻 실행 및 디버그 (Usage & Debug)
1. 기본 실행을 위해 `HyperPack.exe`를 더블클릭합니다.
2. 만약 실행 중 문제가 발생하거나 초기화에 실패할 경우, `Run_Debug.bat`를 실행하여 콘솔 메시지를 확인해 주세요.
3. 문제 발생 시 로그 파일 위치: `%TEMP%\HyperPack_startup.log`

## 🤝 기여 및 피드백 (Contributing)
HyperPack은 오픈소스 프로젝트입니다. 특히 **대용량 파일 압축 시의 안정성 테스트, 7z 대비 압축 성능 개선을 위한 알고리즘 제안, 혹은 코드 리팩토링**에 대한 기여를 언제나 환영합니다. 
버그를 발견하시거나 제안 사항이 있다면 언제든 [Issues] 탭에 등록해 주세요!

## 📄 라이선스 (License)
본 프로젝트는 [MIT License](LICENSE)에 따라 자유롭게 복제, 수정, 배포 및 상업적 이용이 가능합니다.
