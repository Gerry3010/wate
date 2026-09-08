WAILS3 ?= $(shell command -v wails3 2>/dev/null || echo $(HOME)/go/bin/wails3)

.PHONY: build dev run test lint frontend-deps clean install

build: ## Production build → bin/yate
	$(WAILS3) build

dev: ## Dev mode with hot reload
	$(WAILS3) dev

run: build
	./bin/yate

frontend-deps:
	cd frontend && npm install

test: ## Go + frontend tests (needs a built frontend for the embed)
	@test -d frontend/dist || $(MAKE) build
	go test ./...
	cd frontend && npm test

lint:
	go vet ./...
	cd frontend && npm run typecheck

install: build ## Install to ~/.local
	install -Dm755 bin/yate $(HOME)/.local/bin/yate
	install -Dm644 build/linux/desktop $(HOME)/.local/share/applications/io.github.gerry3010.yate.desktop
	install -Dm644 build/appicon.png $(HOME)/.local/share/icons/hicolor/512x512/apps/yate.png
	sed -i 's|^Exec=.*|Exec=$(HOME)/.local/bin/yate %u|' $(HOME)/.local/share/applications/io.github.gerry3010.yate.desktop

clean:
	rm -rf bin frontend/dist
