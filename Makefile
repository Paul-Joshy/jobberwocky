.PHONY: build test live vet

build:
	go build -o bin/jobberwocky .

test: vet
	go test ./...

live:
	JOBBERWOCKY_LIVE=1 go test ./...

vet:
	gofmt -l . | (! grep .) && go vet ./...
