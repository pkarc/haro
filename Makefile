VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build web test golden lint run clean proto

build: ## Compila el binario (incluye la web si ya está en web/dist)
	go build -ldflags "$(LDFLAGS)" -o bin/haro ./cmd/haro

web: ## Compila la interfaz React en web/dist
	cd web && npm install && npm run build

proto: ## Regenera el código de gen/ y web/src/gen desde proto/ (requiere buf y npm install en web/)
	buf lint && buf generate

test: ## Ejecuta los tests del núcleo y del CLI
	go test ./...

golden: ## Regenera los casos esperados tras un cambio intencionado
	go test ./core -update

lint: ## Analiza los casos de prueba con el propio haro
	go run ./cmd/haro lint core/testdata/example.cfg

run: build ## Abre la interfaz local con la configuración de ejemplo
	./bin/haro serve --file core/testdata/example.cfg

clean:
	rm -rf bin web/node_modules && find web/dist -mindepth 1 ! -name .keep -delete
