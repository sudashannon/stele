.PHONY: build dev test clean

build:
	cd web && npm ci && npm run build
	go build -o stele .

dev:
	@echo "Run in two terminals:"
	@echo "  1) cd web && npm run dev"
	@echo "  2) go run ."

test:
	cd web && npm run test
	go test ./...

clean:
	rm -rf web/dist web/node_modules stele
