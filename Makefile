

.PHONY: run
run:
	go run ./cmd/hfd

.PHONY: update-hf-api-status
update-hf-api-status:
	go run ./hack/update-hf-api-status
