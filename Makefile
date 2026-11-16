VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build web wasm wasm-tiny test golden lint run clean proto

build: ## Compila el binario (incluye la web si ya está en web/dist)
	go build -ldflags "$(LDFLAGS)" -o bin/haro ./cmd/haro

web: ## Compila la interfaz React en web/dist
	cd web && npm install && npm run build

proto: ## Regenera el código de gen/ y web/src/gen desde proto/ (requiere buf y npm install en web/)
	buf lint && buf generate

wasm: ## Compila el núcleo a WebAssembly en web/public/wasm (para la web sin servidor)
	mkdir -p web/public/wasm
	GOOS=js GOARCH=wasm go build -trimpath -ldflags "-s -w" -o web/public/wasm/haro.wasm ./wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/public/wasm/wasm_exec.js

wasm-tiny: ## Como wasm, pero con TinyGo (en Docker): ~1,8 MB en vez de ~6 MB
	mkdir -p web/public/wasm
	docker run --rm -v "$$PWD":/src -w /src tinygo/tinygo:latest sh -c 'tinygo build -o /src/web/public/wasm/haro.wasm -target wasm -no-debug ./wasm && cp /usr/local/tinygo/targets/wasm_exec.js /src/web/public/wasm/wasm_exec.js'

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
