# rsrc -manifest timesync.xml <exe>

APP        ?= timesync
GO         ?= go
JQ         ?= jq
RSRC       ?= rsrc
WINDOWS_CC ?= x86_64-w64-mingw32-gcc

BIN_DIR    ?= bin
GOARCH     ?= amd64
GO_SRC ?= ./cmd/

JSON_SRC := default.json
JSON_DST := timesync/embedded.json
MANIFEST := timesync.xml
SYSO := $(GO_SRC)/rsrc_windows_$(GOARCH).syso

BUILDFLAGS := -trimpath
LDFLAGS    := -s -w

GO_SRCS    := $(shell find . -name '*.go' -not -path './vendor/*')

all: build-linux build-win

$(JSON_DST): $(JSON_SRC)
	@echo ">> minify JSON: $< -> $@"
	@mkdir -p $(dir $@)
	@$(JQ) -c . $< > $@
	@printf "   size: %s bytes\n" "$$(wc -c < $@)"

$(SYSO): $(MANIFEST)
	@echo ">> rsrc: $< -> $@"
	@command -v $(RSRC) >/dev/null 2>&1 || { \
		echo "   installing rsrc..."; \
		$(GO) install github.com/akavel/rsrc@latest; }
	@$(RSRC) -arch $(GOARCH) -manifest $< -o $@

build-linux: $(JSON_DST) $(GO_SRCS)
	@echo ">> build linux/$(GOARCH)"
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) \
		$(GO) build $(BUILDFLAGS) -ldflags="$(LDFLAGS)" \
		-o $(BIN_DIR)/$(APP)-linux-$(GOARCH) $(GO_SRC)

build-win: $(JSON_DST) $(SYSO) $(GO_SRCS)
	@echo ">> build windows/$(GOARCH)"
	@command -v $(WINDOWS_CC) >/dev/null 2>&1 || { \
		echo "error: Windows C compiler '$(WINDOWS_CC)' was not found; install MinGW-w64 or set WINDOWS_CC" >&2; \
		exit 1; }
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=1 GOOS=windows GOARCH=$(GOARCH) CC=$(WINDOWS_CC) \
		$(GO) build $(BUILDFLAGS) -ldflags="$(LDFLAGS)" \
		-o $(BIN_DIR)/$(APP)-windows-$(GOARCH).exe $(GO_SRC)

clean:
	@rm -f $(JSON_DST) $(SYSO)
	@rm -rf $(BIN_DIR)

test:
	@$(GO) test ./...

.PHONY: all clean test tools
