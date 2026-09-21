# HyperPack v0.3.2 Xtreme+

이번 버전은 압축률을 실제로 끌어내리기 위한 실험용 고압축 엔진입니다.

## 핵심 변경

- HPK4 포맷으로 분리
- Level 9에서 128 MiB LZ dictionary
- 8 MiB 그룹 단위의 solid-friendly 스트림
- 최대 64 KiB match
- hash bucket 19-bit + bucket당 최대 32 후보
- distance / length VarInt
- 높은 레벨에서 1-byte lazy lookahead
- 커스텀 LZ 토큰을 DEFLATE BestCompression으로 2차 압축
- 압축 가능성이 낮으면 raw 그룹으로 자동 fallback
- 여러 작업을 동시에 처리해 멀티코어 활용
- OpenCL GPU가 있으면 8-byte hash 사전 계산을 GPU로 보조하는 실험 경로
- GPU가 없거나 오류가 나면 CPU만 사용
- 대용량 입력은 전체를 RAM에 올리지 않고 스트리밍

## GPU

GPU는 필수가 아닙니다. `OpenCL.dll`을 찾고 GPU 장치를 확인할 수 있을 때만 hash precomputation 보조에 사용합니다.

GPU 경로를 비활성화하려면:

```bat
set HYPERPACK_NO_GPU=1
HyperPack.exe
```

GPU 경로는 아직 실험적이므로 실제 압축 시간은 GPU/드라이버에 따라 달라질 수 있습니다.

## 실행

`HyperPack.exe`를 더블클릭합니다.

일반 사용자는 C++, CMake, Python을 설치할 필요가 없습니다.

`HyperPack_debug.exe`는 콘솔이 표시되는 개발/진단용 빌드입니다.

## 주의

현재 v0.3.2의 목표는 모든 데이터에서 7z를 무조건 이기는 것이 아니라, 이전 데모보다 훨씬 강한 고압축 엔진을 만드는 것입니다. 실제 1 GB LM Studio 데이터셋과 같은 기준 데이터로 버전별 결과를 비교하는 것을 권장합니다.
