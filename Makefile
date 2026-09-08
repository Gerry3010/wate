WAILS3 ?= $(shell command -v wails3 2>/dev/null || echo $(HOME)/go/bin/wails3)

.PHONY: build dev run test lint frontend-deps clean install install-nautilus

build: ## Production build → bin/wate
	$(WAILS3) build

dev: ## Dev mode with hot reload
	$(WAILS3) dev

run: build
	./bin/wate

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
	install -Dm755 bin/wate $(HOME)/.local/bin/wate
	install -Dm644 build/linux/desktop $(HOME)/.local/share/applications/io.github.gerry3010.wate.desktop
	install -Dm644 build/appicon.png $(HOME)/.local/share/icons/hicolor/512x512/apps/wate.png
	sed -i 's|^Exec=.*|Exec=$(HOME)/.local/bin/wate %f|' $(HOME)/.local/share/applications/io.github.gerry3010.wate.desktop
	-update-desktop-database $(HOME)/.local/share/applications 2>/dev/null

install-nautilus: ## "Open in wate" in the Nautilus context menu (needs nautilus-python)
	install -Dm644 contrib/nautilus/wate-nautilus.py $(HOME)/.local/share/nautilus-python/extensions/wate-nautilus.py
	-nautilus -q 2>/dev/null

clean:
	rm -rf bin frontend/dist
