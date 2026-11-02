# NodeInventory

NodeInventory는 승인된 Node 기준과 관측 상태를 비교하는 관측 전용 controller다. Node를 수정하거나 reboot sentinel을 만들지 않는다. `pkg/gate`는 기존 bridge가 sentinel을 새로 만들기 직전에 선택적으로 호출하는 라이브러리이며 기본 비활성이다. 실제 bridge 코드는 이 저장소에 없으므로 적용 패치는 제공하지 않는다.

## 개발과 배포 준비

Go 1.24와 Kubernetes API 접근 환경이 필요하다. `make generate`, `make test`, `make build`로 생성, 테스트, 빌드한다. 의존성은 `go.mod`/`go.sum`에 고정되어 있다. controller-runtime v0.20.4와 Kubernetes Go API v0.32.4를 사용한다. 다른 Kubernetes 버전과의 실제 호환성은 대상 클러스터에서 검증해야 한다. Node lifecycle condition은 향후 확장 시 고려하되 현재 버전에서는 읽거나 status에 추가하지 않는다. [공식 문서](https://kubernetes.io/docs/reference/node/node-lifecycle-conditions/)에 따르면 Kubernetes 1.37의 NodeLifecycleConditions는 alpha, 기본 비활성이며 조건 설정 자체가 노드 동작을 변경하지 않는다. 이 controller는 feature gate나 Node condition을 설정하지 않는다.

배포는 명시적 승인 후에만 수행한다. 이미지를 `docker build -t <registry>/node-inventory:<tag> .`로 만들고 `config/deploy/controller.yaml`의 이미지를 교체한다. `kubectl apply -f config/crd`, `kubectl apply -f config/rbac/controller.yaml`, `kubectl apply -f config/deploy/controller.yaml` 순서로 설치한다. controller는 leader election을 기본 활성화한다. 여러 replica는 동일 namespace의 Lease를 공유한다. 업데이트 주기는 `--interval`이며 기본 1분이다. Node watch 이벤트로도 갱신한다. status만 변경된 inventory 이벤트는 재처리하지 않는다.

삭제 시 먼저 bridge gate를 비활성화하고 controller Deployment를 제거한다. 보존이 필요한 inventory를 백업한 다음 inventory와 RBAC를 제거하고 마지막으로 CRD를 삭제한다. CRD 삭제는 모든 inventory를 삭제한다. Namespace를 다른 용도로 사용하면 삭제하지 않는다.

## 기준 승인

`kubectl get node <name> -o yaml`에서 `metadata.uid`, `status.nodeInfo.operatingSystem`, `architecture`, `kernelVersion`, `status.capacity.cpu`, `memory` 및 필요한 GPU resource를 확인한다. `config/samples/inventory.yaml`을 복사해 이름, UID, 승인한 값과 UTC `capturedAt`을 채우고 운영자가 검토 후 적용한다. GPU 키는 실제 장치 플러그인의 resource 이름을 사용한다. 지정된 capacity 키만 비교하며 CPU와 memory는 필수다. 기준은 자동 생성 또는 변경되지 않는다. 변경 시 원인과 영향 확인 후 spec을 명시적으로 수정한다. 같은 이름의 Node가 재생성되면 UID를 검토하고 새로운 기준을 승인해야 한다. Quantity는 Kubernetes quantity로 보존하고 값으로 비교한다. 커널이나 capacity 차이는 관측 신호이며 재부팅을 유발하지 않는다.

`kubectl get nodeinventory`와 `kubectl get nodeinventory <name> -o yaml`에서 `ObservationAvailable`, `BaselineMatched` 및 `observedAt`을 확인한다. Node가 없거나 UID가 다르거나 읽기 실패 시 관측값은 제거되고 조건은 Unknown이다. API 서버 자체가 상태 쓰기를 거부하거나 controller가 멈추면 이전 status가 남을 수 있으므로 소비자는 반드시 `observedAt`과 최대 허용 age를 검사한다. stale 시 controller/leader와 API 연결을 복구하고 새 관측을 확인한다. 적절한 max age는 갱신 주기, API 지연과 장애 허용 시간에 맞춰 운영자가 결정한다. 시계 동기화도 필요하다.

## bridge 적용 지점

기존 condition persistence, cooldown, disable annotation을 통과한 뒤 **새 sentinel 생성 직전**에 `gate.Gate{Enabled: true, Reader: apiReader, MaxAge: ...}.Check(ctx, node.Name, string(node.UID))`를 호출한다. `Allowed`가 false면 `Reason`을 기록하고 이번 생성만 보류한다. `BaselineMatched`는 로그용 관측 정보다. 이 경로는 fault timer와 cooldown을 초기화하거나 성공으로 기록하지 않아야 한다. 기존 dry-run 분기와 sentinel 존재 확인을 그대로 유지하고, gate는 기존 sentinel을 삭제하지 않는다. 예를 들어 dry-run은 gate 통과 후에도 실제 파일을 생성하지 않아야 한다. 이 라이브러리는 파일을 만지지 않으며 다른 생산자가 만든 sentinel을 차단할 수 없다. 기존 운영 sentinel 경로는 `/var/run/fai-kured-reboot-required`다.

bridge에 `config/rbac/bridge-role.yaml`의 get 권한을 별도 ClusterRoleBinding으로 부여한다. 공유 service account의 cluster-scoped RBAC만으로 각 bridge가 자기 노드의 inventory만 읽도록 강제할 수 없다. 더 강한 격리는 별도 인증/인가 설계가 필요하다. 실제 bridge 소스를 확보한 후 원래의 timer, cooldown, dry-run, sentinel 동작과 통합 테스트해야 한다.

kured annotation, 로그 부재, `spec.unschedulable`은 drain 완료나 reboot 성공의 증거가 아니다. 성공 판정은 범위 밖이며 별도로 boot ID 변경과 Ready 복귀 등 증거가 필요하다. kind의 모의 reboot은 실제 reboot이 아니다.

## NodeRepair 미리보기

`NodeRepair`는 현재 **읽기 전용 dry-run만** 지원한다. `config/samples/repair.yaml`의 노드 이름과 UID를 지정해 생성하면 controller가 노드에 배치된 Pod와 같은 namespace의 일치하는 PDB를 관측해 `status.preview`에 기록한다. mirror Pod와 DaemonSet 소유 Pod는 제외 대상으로 분류하고, 삭제 중인 Pod는 별도로 표시한다. PDB의 `disruptionsAllowed`와 generation은 참고 정보이며 eviction의 성공 또는 차단 원인을 보장하지 않는다. Node UID가 바뀌거나 API 조회가 실패하면 preview를 제거하고 `PreviewAvailable=Unknown`으로 표시한다. controller가 멈추거나 status 쓰기가 실패하면 이전 status가 남을 수 있으므로 `observedAt`과 condition의 `observedGeneration`을 확인한다. 200개가 넘는 Pod는 불완전한 결과를 제공하지 않고 보류한다.

이 단계는 Node, Pod 또는 PDB를 수정하지 않는다. 단일 노드 실제 drain, 복구 작업의 소유권 및 동시성, DrainPolicy, rollout과 reboot은 아직 구현되지 않았다. 샘플과 CRD 설치 후 `kubectl get noderepairs` 및 `kubectl get noderepair <name> -o yaml`로 확인할 수 있다. 설치나 클러스터 변경은 별도 승인 후 진행한다.
