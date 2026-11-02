.PHONY: test build generate
build:
	go build ./cmd/controller
test:
	go test ./...
generate:
	go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.17.2 object:headerFile=/dev/null crd paths=./api/... output:crd:artifacts:config=config/crd
