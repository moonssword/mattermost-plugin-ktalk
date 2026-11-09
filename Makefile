PLUGIN_ID      := konturtalk
PLUGIN_VERSION := $(shell sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' plugin.json | head -1)
BUNDLE         := dist/$(PLUGIN_ID)-$(PLUGIN_VERSION).tar.gz

# Go is not required locally: the server is built and tested in a container.
GO_IMAGE       := golang:1.26-alpine
GO_VOLUME      ?= ktalk-gomod
GO_RUN         := docker run --rm -v "$(CURDIR)/server":/src -w /src -v $(GO_VOLUME):/go/pkg/mod $(GO_IMAGE)
MM_CONTAINER   ?= mattermost-mattermost-1
MMCTL          := docker exec $(MM_CONTAINER) /mattermost/bin/mmctl --local

.PHONY: all server webapp test lint bundle deploy talk-check clean

all: bundle

server:
	$(GO_RUN) sh -c '\
		go vet ./... && \
		for arch in amd64 arm64; do \
			CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags="-s -w" -o dist/plugin-linux-$$arch . || exit 1; \
		done'

webapp:
	cd webapp && npm ci --no-audit --no-fund && npm run check-types && npm test && npm run build

test:
	$(GO_RUN) go test ./...
	cd webapp && npm test

lint:
	docker run --rm -v "$(CURDIR)/server":/src -w /src -v $(GO_VOLUME):/go/pkg/mod -e GOFLAGS=-mod=mod -e GOLANGCI_LINT_CACHE=/tmp/lintcache golangci/golangci-lint:latest golangci-lint run ./...
	cd webapp && npm run check-types

bundle: server webapp
	rm -rf dist && mkdir -p dist/$(PLUGIN_ID)/server dist/$(PLUGIN_ID)/webapp
	cp plugin.json dist/$(PLUGIN_ID)/
	cp -r assets dist/$(PLUGIN_ID)/
	cp -r server/dist dist/$(PLUGIN_ID)/server/
	cp -r webapp/dist dist/$(PLUGIN_ID)/webapp/
	# COPYFILE_DISABLE stops macOS tar from adding ._* AppleDouble entries,
	# which make Mattermost fail to find the manifest.
	cd dist && COPYFILE_DISABLE=1 tar -czf $(PLUGIN_ID)-$(PLUGIN_VERSION).tar.gz $(PLUGIN_ID)
	@echo "Bundle: $(BUNDLE)"

# Installs into the local Docker Mattermost via mmctl local mode.
deploy: bundle
	docker cp $(BUNDLE) $(MM_CONTAINER):/tmp/$(PLUGIN_ID).tar.gz
	$(MMCTL) plugin add --force /tmp/$(PLUGIN_ID).tar.gz
	$(MMCTL) plugin enable $(PLUGIN_ID)

# Shows the expiry and scopes of the key in .env.
talk-check:
	@scripts/talk-check.sh

clean:
	rm -rf dist server/dist webapp/dist webapp/node_modules
