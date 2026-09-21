WAILS3 ?= $(shell command -v wails3 2>/dev/null || echo $(HOME)/go/bin/wails3)

.PHONY: build dev run test lint frontend-deps icons clean install install-nautilus

build: ## Production build → bin/wate
	$(WAILS3) build

dev: ## Dev mode with hot reload
	$(WAILS3) dev

run: build
	./bin/wate

frontend-deps:
	cd frontend && npm install

# The macOS app icon lives in build/appicon.icon (Icon Composer layers); the packaged bundle reads
# the compiled build/darwin/Assets.car, which is checked in because Linux builds cannot produce it.
# Rebuild it here after touching the .icon, then commit the result.
icons: ## Recompile build/darwin/Assets.car from build/appicon.icon (macOS, needs Xcode)
	@tmp=$$(mktemp -d); \
	actool build/appicon.icon --compile "$$tmp" --app-icon appicon --platform macosx \
		--minimum-deployment-target 12.0 --output-partial-info-plist "$$tmp/partial.plist" >/dev/null; \
	cp "$$tmp/Assets.car" build/darwin/Assets.car; \
	rm -rf "$$tmp"; \
	echo "build/darwin/Assets.car rebuilt from build/appicon.icon"

test: ## Go + frontend tests (needs a built frontend for the embed)
	@test -d frontend/dist || $(MAKE) build
	go test ./...
	cd frontend && npm test

lint:
	go vet ./...
	cd frontend && npm run typecheck

install: build ## Install to ~/.local
	install -Dm755 bin/wate $(HOME)/.local/bin/wate
	install -Dm644 build/linux/desktop $(HOME)/.local/share/applications/org.wails.wate.desktop
	install -Dm644 build/appicon.png $(HOME)/.local/share/icons/hicolor/512x512/apps/wate.png
	sed -i 's|^Exec=.*|Exec=$(HOME)/.local/bin/wate %f|' $(HOME)/.local/share/applications/org.wails.wate.desktop
	-update-desktop-database $(HOME)/.local/share/applications 2>/dev/null

install-nautilus: ## "Open in wate" in the Nautilus context menu (needs nautilus-python)
	install -Dm644 contrib/nautilus/wate-nautilus.py $(HOME)/.local/share/nautilus-python/extensions/wate-nautilus.py
	-nautilus -q 2>/dev/null

clean:
	rm -rf bin frontend/dist
