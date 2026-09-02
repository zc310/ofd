GO ?= go
ZIP ?= zip

GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
CGO_ENABLED ?= 1
CC := $(shell $(GO) env CC)

ifeq ($(GOOS),windows)
ifeq ($(GOARCH),amd64)
CC := x86_64-w64-mingw32-gcc
endif
ifeq ($(GOARCH),386)
CC := i686-w64-mingw32-gcc
endif
ifeq ($(GOARCH),arm64)
CC := aarch64-w64-mingw32-gcc
endif
endif

PLATFORM := $(GOOS)-$(GOARCH)
BIN_SUFFIX := $(if $(filter windows,$(GOOS)),.exe,)
VIEWER_LDFLAGS := $(if $(filter windows,$(GOOS)),-ldflags "-H=windowsgui",)
DIST_DIR := dist
BUILD_DIR := .build
BIN_DIR := $(BUILD_DIR)/bin/$(PLATFORM)
PACKAGE_DIR := $(BUILD_DIR)/packages/$(PLATFORM)

VIEWER := $(BIN_DIR)/ofd-viewer$(BIN_SUFFIX)
CONVERTER := $(BIN_DIR)/ofd-converter$(BIN_SUFFIX)
THUMBNAILER := $(BIN_DIR)/ofd-thumbnailer$(BIN_SUFFIX)

VIEWER_PACKAGE := $(DIST_DIR)/ofd-viewer-$(PLATFORM).zip
CONVERTER_PACKAGE := $(DIST_DIR)/ofd-converter-$(PLATFORM).zip
THUMBNAILER_PACKAGE := $(DIST_DIR)/ofd-thumbnailer-$(PLATFORM).zip

.PHONY: all build package package-viewer package-converter package-thumbnailer clean help FORCE

all: package

help:
	@printf '%s\n' \
		'make build                    Build all command programs' \
		'make package                  Build three independent ZIP packages' \
		'make package-viewer           Build the OFD viewer package' \
		'make package-converter       Build the OFD converter package' \
		'make package-thumbnailer     Build the OFD thumbnailer package' \
		'make clean                   Remove generated build files and packages' \
		'' \
		'Cross compilation variables:' \
		'  GOOS=linux GOARCH=amd64 CGO_ENABLED=1 make package' \
		'  ofd-thumbnailer is built only when GOOS=linux'

build: $(VIEWER) $(CONVERTER) $(if $(filter linux,$(GOOS)),$(THUMBNAILER))

$(VIEWER): FORCE
	@mkdir -p "$(BIN_DIR)"
	@if [ "$(GOOS)" = "windows" ] && ! command -v "$(CC)" >/dev/null 2>&1; then echo "错误: 找不到 Windows CGO 编译器 $(CC)，请安装 MinGW-w64 或通过 CC 指定编译器。" >&2; exit 1; fi
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(VIEWER_LDFLAGS) -o "$@" ./cmd/ofd-viewer

$(CONVERTER): FORCE
	@mkdir -p "$(BIN_DIR)"
	@if [ "$(GOOS)" = "windows" ] && ! command -v "$(CC)" >/dev/null 2>&1; then echo "错误: 找不到 Windows CGO 编译器 $(CC)，请安装 MinGW-w64 或通过 CC 指定编译器。" >&2; exit 1; fi
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -o "$@" ./cmd/ofd-converter

ifeq ($(GOOS),linux)
$(THUMBNAILER): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -o "$@" ./cmd/ofd-thumbnailer
endif

package: package-viewer package-converter $(if $(filter linux,$(GOOS)),package-thumbnailer)

package-viewer: $(VIEWER_PACKAGE)

$(VIEWER_PACKAGE): $(VIEWER) cmd/ofd-viewer/README.md
	@mkdir -p "$(DIST_DIR)"
	@rm -rf "$(PACKAGE_DIR)/ofd-viewer"
	@mkdir -p "$(PACKAGE_DIR)/ofd-viewer"
	@rm -f "$@"
	@cp "$(VIEWER)" "$(PACKAGE_DIR)/ofd-viewer/ofd-viewer$(BIN_SUFFIX)"
	@cp "cmd/ofd-viewer/README.md" "$(PACKAGE_DIR)/ofd-viewer/README.md"
	@cd "$(PACKAGE_DIR)" && "$(ZIP)" -qr "$(abspath $@)" "ofd-viewer"

package-converter: $(CONVERTER_PACKAGE)

$(CONVERTER_PACKAGE): $(CONVERTER) cmd/ofd-converter/README.md
	@mkdir -p "$(DIST_DIR)"
	@rm -rf "$(PACKAGE_DIR)/ofd-converter"
	@mkdir -p "$(PACKAGE_DIR)/ofd-converter"
	@rm -f "$@"
	@cp "$(CONVERTER)" "$(PACKAGE_DIR)/ofd-converter/ofd-converter$(BIN_SUFFIX)"
	@cp "cmd/ofd-converter/README.md" "$(PACKAGE_DIR)/ofd-converter/README.md"
	@cd "$(PACKAGE_DIR)" && "$(ZIP)" -qr "$(abspath $@)" "ofd-converter"

ifeq ($(GOOS),linux)

package-thumbnailer: $(THUMBNAILER_PACKAGE)

$(THUMBNAILER_PACKAGE): $(THUMBNAILER) cmd/ofd-thumbnailer/README.md cmd/ofd-thumbnailer/install.sh cmd/ofd-thumbnailer/ofd.thumbnailer
	@mkdir -p "$(DIST_DIR)"
	@rm -rf "$(PACKAGE_DIR)/ofd-thumbnailer"
	@mkdir -p "$(PACKAGE_DIR)/ofd-thumbnailer"
	@rm -f "$@"
	@cp "$(THUMBNAILER)" "$(PACKAGE_DIR)/ofd-thumbnailer/ofd-thumbnailer$(BIN_SUFFIX)"
	@cp "cmd/ofd-thumbnailer/README.md" "$(PACKAGE_DIR)/ofd-thumbnailer/README.md"
	@cp "cmd/ofd-thumbnailer/ofd.thumbnailer" "$(PACKAGE_DIR)/ofd-thumbnailer/ofd.thumbnailer"
	@cp "cmd/ofd-thumbnailer/install.sh" "$(PACKAGE_DIR)/ofd-thumbnailer/install.sh"
	@chmod 755 "$(PACKAGE_DIR)/ofd-thumbnailer/install.sh"
	@cd "$(PACKAGE_DIR)" && "$(ZIP)" -qr "$(abspath $@)" "ofd-thumbnailer"
else

package-thumbnailer:
	@echo "错误: ofd-thumbnailer 仅支持编译 Linux 版本。" >&2
	@exit 1
endif

clean:
	@rm -rf "$(BUILD_DIR)" "$(DIST_DIR)"

FORCE:
