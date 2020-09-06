build:
	go build -o ggif cmd/ggif
.PHONY: build

install:
	go install ./cmd/ggif
.PHONY: install
