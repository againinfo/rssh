ifdef RSSH_HOMESERVER
	LDFLAGS += -X main.destination=$(RSSH_HOMESERVER)
endif

ifdef RSSH_FINGERPRINT
	LDFLAGS += -X main.fingerprint=$(RSSH_FINGERPRINT)
endif

ifdef RSSH_PROXY
	LDFLAGS += -X main.proxy=$(RSSH_PROXY)
endif

ifdef IGNORE
	LDFLAGS += -X main.ignoreInput=$(IGNORE)
endif

ifndef CGO_ENABLED
	export CGO_ENABLED=0
endif

BUILD_FLAGS := -trimpath

LDFLAGS += -X 'rssh/internal.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)'

LDFLAGS_RELEASE = $(LDFLAGS) -s -w

debug: .generate_keys
	go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS)" -o bin ./...
	GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin ./...

build: release

release: .generate_keys 
	go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin ./...
	GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin ./...

e2e: .generate_keys
	go build -ldflags="rssh/e2e.Version=$(shell git describe --tags)" -o e2e ./...
	cp internal/client/keys/private_key.pub e2e/authorized_controllee_keys

client: .generate_keys
	go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin ./cmd/client

client_windows: .generate_keys
	GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin/client_windows_amd64.exe ./cmd/client

client_windows_arm64: .generate_keys
	GOOS=windows GOARCH=arm64 go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin/client_windows_arm64.exe ./cmd/client

client_dll: .generate_keys
	test -n "$(RSSH_HOMESERVER)" # Shared objects cannot take arguments, so must have a callback server baked in (define RSSH_HOMESERVER)
	CGO_ENABLED=1 go build $(BUILD_FLAGS) -tags=cshared -buildmode=c-shared -ldflags="$(LDFLAGS_RELEASE)" -o bin/client.dll ./cmd/client

server:
	mkdir -p bin
	go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin ./cmd/server

server_windows:
	mkdir -p bin
	GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin/server_windows_amd64.exe ./cmd/server

server_windows_arm64:
	mkdir -p bin
	GOOS=windows GOARCH=arm64 go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin/server_windows_arm64.exe ./cmd/server

gui:
	mkdir -p bin
	CGO_ENABLED=1 go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin/rssh-gui ./cmd/gui

.generate_keys:
	mkdir -p bin
# Only generate a key if one doesn't already exist (avoid interactive overwrite prompt).
	test -f internal/client/keys/private_key || ssh-keygen -t ed25519 -N '' -C '' -f internal/client/keys/private_key
# Avoid duplicate entries
	touch bin/authorized_controllee_keys
	@grep -q "$$(cat internal/client/keys/private_key.pub)" bin/authorized_controllee_keys || cat internal/client/keys/private_key.pub >> bin/authorized_controllee_keys
	touch authorized_controllee_keys
	@grep -q "$$(cat internal/client/keys/private_key.pub)" authorized_controllee_keys || cat internal/client/keys/private_key.pub >> authorized_controllee_keys
