.PHONY: build test run dev clean

## build: compile the server binary into ./bin/fibr
build:
	cd server && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o ../bin/fibr ./cmd/fibr

## test: run unit tests
test:
	cd server && go test ./...

## run: run the server locally in dev mode (no kernel WireGuard needed)
run: build
	mkdir -p data
	FIBR_DATA_DIR=./data FIBR_ADMIN_TOKEN=dev-admin-token FIBR_SECRET=dev-secret FIBR_DOMAIN=localhost FIBR_WG_HOST=127.0.0.1 ./bin/fibr

## dev: run with hot test values + exposed port, then open http://127.0.0.1:8080/api/health
dev: run

clean:
	rm -rf bin data
