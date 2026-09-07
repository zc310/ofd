GO ?= go
ZIP ?= zip
FYNE ?= fyne

GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
ARM64_GOOS ?= linux
WINDOWS_CC ?= x86_64-w64-mingw32-gcc
CGO_ENABLED ?= 1
CC := $(shell $(GO) env CC)
GOHOSTOS := $(shell $(GO) env GOHOSTOS)

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
VALIDATOR := $(BIN_DIR)/ofd-validator$(BIN_SUFFIX)
ANALYZER := $(BIN_DIR)/ofd-analyzer$(BIN_SUFFIX)

VIEWER_PACKAGE := $(DIST_DIR)/ofd-viewer-$(PLATFORM).zip
CONVERTER_PACKAGE := $(DIST_DIR)/ofd-converter-$(PLATFORM).zip
THUMBNAILER_PACKAGE := $(DIST_DIR)/ofd-thumbnailer-$(PLATFORM).zip
VALIDATOR_PACKAGE := $(DIST_DIR)/ofd-validator-$(PLATFORM).zip
ANALYZER_PACKAGE := $(DIST_DIR)/ofd-analyzer-$(PLATFORM).zip
ANDROID_VIEWER_PACKAGE := $(DIST_DIR)/ofd-viewer-android.apk
ANDROID_VIEWER_ZIP := $(DIST_DIR)/ofd-viewer-android.zip
ANDROID_VIEWER_APP_ID := github.com.zc310.ofd.viewer
ANDROID_VIEWER_NAME := OFD Viewer
ANDROID_VIEWER_OUTPUT := OFD_Viewer.apk
VIEWER_VERSION ?= 0.0.5
ANDROID_VIEWER_SOURCES := $(filter-out %_test.go,$(wildcard cmd/ofd-viewer/*.go))

TOOL_BUILD_TARGETS := $(CONVERTER) $(VALIDATOR) $(ANALYZER) $(if $(filter linux,$(GOOS)),$(THUMBNAILER))
TOOL_PACKAGE_TARGETS := package-converter package-validator package-analyzer $(if $(filter linux,$(GOOS)),package-thumbnailer)
VIEWER_BUILD_TARGETS := $(if $(or $(and $(filter linux,$(GOOS)),$(filter arm64,$(GOARCH))),$(and $(filter darwin,$(GOOS)),$(filter linux,$(GOHOSTOS)))),,$(VIEWER))
VIEWER_PACKAGE_TARGETS := $(if $(or $(and $(filter linux,$(GOOS)),$(filter arm64,$(GOARCH))),$(and $(filter darwin,$(GOOS)),$(filter linux,$(GOHOSTOS)))),,package-viewer)
WINDOWS_BUILD_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),build-windows-amd64,)
WINDOWS_PACKAGE_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),package-windows-amd64,)
DARWIN_CROSS_BUILD_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),build-darwin-arm64 build-darwin-amd64,)
DARWIN_CROSS_PACKAGE_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),package-darwin-arm64 package-darwin-amd64,)
WINDOWS_ARM64_BUILD_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),build-windows-arm64,)
WINDOWS_ARM64_PACKAGE_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),package-windows-arm64,)
LINUX_ARM64_BUILD_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),build-arm64,)
LINUX_ARM64_PACKAGE_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),package-arm64,)
DARWIN_CGO_ENABLED := $(if $(filter linux,$(GOHOSTOS)),0,$(CGO_ENABLED))
DARWIN_BUILD_TARGET := $(if $(filter linux,$(GOHOSTOS)),build-tools,build)
DARWIN_PACKAGE_TARGET := $(if $(filter linux,$(GOHOSTOS)),package-tools,package)

.PHONY: all build build-tools build-arm64 build-darwin-arm64 build-darwin-amd64 build-windows-amd64 build-windows-arm64 package package-desktop package-tools package-arm64 package-darwin-arm64 package-darwin-amd64 package-windows-amd64 package-windows-arm64 package-viewer package-viewer-android package-viewer-android-zip package-converter package-thumbnailer package-validator package-analyzer clean help FORCE

all: package

help:
	@printf '%s\n' \
		'make build                    Build all command programs' \
		'make package                  Build desktop packages and Android APK ZIP' \
		'make package-viewer           Build the OFD viewer package' \
		'make package-viewer-android   Build the OFD viewer Android APK' \
		'make package-viewer-android-zip Build the OFD viewer Android ZIP package' \
		'make package-converter       Build the OFD converter package' \
		'make package-validator       Build the OFD validator' \
		'make package-analyzer        Build the OFD analyzer' \
		'make package-thumbnailer     Build the OFD thumbnailer package' \
		'make build-arm64             Build Linux ARM64 command programs' \
		'make package-arm64           Build Linux ARM64 packages' \
		'make build-darwin-arm64      Build macOS ARM64 command programs' \
		'make package-darwin-arm64    Build macOS ARM64 packages' \
		'make build-darwin-amd64      Build macOS x86_64 command programs' \
		'make package-darwin-amd64    Build macOS x86_64 packages' \
		'make build-windows-amd64     Build Windows x86_64 command programs' \
		'make package-windows-amd64   Build Windows x86_64 packages' \
		'make build-windows-arm64     Build Windows ARM64 command programs' \
		'make package-windows-arm64   Build Windows ARM64 packages' \
		'make clean                   Remove generated build files and packages' \
		'' \
		'Cross compilation variables:' \
		'  GOOS=linux GOARCH=amd64 CGO_ENABLED=1 make package' \
		'  make package-arm64          (default: GOOS=linux GOARCH=arm64)' \
		'  make package-darwin-arm64   (macOS Apple Silicon)' \
		'  make package-darwin-amd64   (macOS Intel)' \
		'  Linux default make also builds macOS ARM64/x86_64 and Windows x86_64/ARM64' \
		'  Linux ARM64 and Linux to macOS builds omit ofd-viewer' \
		'  Android is included in the default package target' \
		'  ofd-thumbnailer is built only when GOOS=linux'

build: $(VIEWER_BUILD_TARGETS) $(TOOL_BUILD_TARGETS) $(WINDOWS_BUILD_TARGETS) $(DARWIN_CROSS_BUILD_TARGETS) $(WINDOWS_ARM64_BUILD_TARGETS) $(LINUX_ARM64_BUILD_TARGETS)

build-tools: $(TOOL_BUILD_TARGETS)

build-arm64:
	$(MAKE) GOOS=$(ARM64_GOOS) GOARCH=arm64 CGO_ENABLED=0 build-tools

build-darwin-arm64:
	$(MAKE) GOOS=darwin GOARCH=arm64 CGO_ENABLED=$(DARWIN_CGO_ENABLED) $(DARWIN_BUILD_TARGET)

build-darwin-amd64:
	$(MAKE) GOOS=darwin GOARCH=amd64 CGO_ENABLED=$(DARWIN_CGO_ENABLED) $(DARWIN_BUILD_TARGET)

build-windows-amd64:
	@if ! command -v "$(WINDOWS_CC)" >/dev/null 2>&1; then echo "错误: 找不到 Windows x86_64 CGO 编译器 $(WINDOWS_CC)，请安装 MinGW-w64 或通过 WINDOWS_CC 指定编译器。" >&2; exit 1; fi
	$(MAKE) GOOS=windows GOARCH=amd64 CC=$(WINDOWS_CC) build

build-windows-arm64:
	$(MAKE) GOOS=windows GOARCH=arm64 CGO_ENABLED=0 CC= build-tools

$(VIEWER): FORCE
	@mkdir -p "$(BIN_DIR)"
	@if [ "$(GOOS)" = "windows" ] && ! command -v "$(CC)" >/dev/null 2>&1; then echo "错误: 找不到 Windows CGO 编译器 $(CC)，请安装 MinGW-w64 或通过 CC 指定编译器。" >&2; exit 1; fi
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(VIEWER_LDFLAGS) -o "$@" ./cmd/ofd-viewer

$(CONVERTER): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -o "$@" ./cmd/ofd-converter

$(VALIDATOR): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -o "$@" ./cmd/ofd-validator

$(ANALYZER): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -o "$@" ./cmd/ofd-analyzer

ifeq ($(GOOS),linux)
$(THUMBNAILER): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -o "$@" ./cmd/ofd-thumbnailer
endif

package: package-desktop package-viewer-android-zip $(WINDOWS_PACKAGE_TARGETS) $(DARWIN_CROSS_PACKAGE_TARGETS) $(WINDOWS_ARM64_PACKAGE_TARGETS) $(LINUX_ARM64_PACKAGE_TARGETS)

package-desktop: $(VIEWER_PACKAGE_TARGETS) $(TOOL_PACKAGE_TARGETS)

package-tools: $(TOOL_PACKAGE_TARGETS)

package-arm64:
	$(MAKE) GOOS=$(ARM64_GOOS) GOARCH=arm64 CGO_ENABLED=0 package-tools

package-darwin-arm64:
	$(MAKE) GOOS=darwin GOARCH=arm64 CGO_ENABLED=$(DARWIN_CGO_ENABLED) $(DARWIN_PACKAGE_TARGET)

package-darwin-amd64:
	$(MAKE) GOOS=darwin GOARCH=amd64 CGO_ENABLED=$(DARWIN_CGO_ENABLED) $(DARWIN_PACKAGE_TARGET)

package-windows-amd64:
	@if ! command -v "$(WINDOWS_CC)" >/dev/null 2>&1; then echo "错误: 找不到 Windows x86_64 CGO 编译器 $(WINDOWS_CC)，请安装 MinGW-w64 或通过 WINDOWS_CC 指定编译器。" >&2; exit 1; fi
	$(MAKE) GOOS=windows GOARCH=amd64 CC=$(WINDOWS_CC) package-desktop

package-windows-arm64:
	$(MAKE) GOOS=windows GOARCH=arm64 CGO_ENABLED=0 CC= package-tools

package-viewer: $(VIEWER_PACKAGE)

$(VIEWER_PACKAGE): $(VIEWER) cmd/ofd-viewer/README.md
	@mkdir -p "$(DIST_DIR)"
	@rm -rf "$(PACKAGE_DIR)/ofd-viewer"
	@mkdir -p "$(PACKAGE_DIR)/ofd-viewer"
	@rm -f "$@"
	@cp "$(VIEWER)" "$(PACKAGE_DIR)/ofd-viewer/ofd-viewer$(BIN_SUFFIX)"
	@cp "cmd/ofd-viewer/README.md" "$(PACKAGE_DIR)/ofd-viewer/README.md"
	@cd "$(PACKAGE_DIR)" && "$(ZIP)" -qr "$(abspath $@)" "ofd-viewer"

package-viewer-android: $(ANDROID_VIEWER_PACKAGE)

$(ANDROID_VIEWER_PACKAGE): $(ANDROID_VIEWER_SOURCES) cmd/ofd-viewer/Icon.png
	@mkdir -p "$(DIST_DIR)"
	@rm -f "$@" "cmd/ofd-viewer/$(ANDROID_VIEWER_OUTPUT)"
	@cd cmd/ofd-viewer && "$(FYNE)" package -os android -appID "$(ANDROID_VIEWER_APP_ID)" -name "$(ANDROID_VIEWER_NAME)" -appVersion "$(VIEWER_VERSION)"
	@mv "cmd/ofd-viewer/$(ANDROID_VIEWER_OUTPUT)" "$@"

package-viewer-android-zip: $(ANDROID_VIEWER_ZIP)

$(ANDROID_VIEWER_ZIP): $(ANDROID_VIEWER_PACKAGE)
	@mkdir -p "$(DIST_DIR)"
	@rm -rf "$(PACKAGE_DIR)/ofd-viewer-android" "$@"
	@mkdir -p "$(PACKAGE_DIR)/ofd-viewer-android"
	@cp "$(ANDROID_VIEWER_PACKAGE)" "$(PACKAGE_DIR)/ofd-viewer-android/ofd-viewer-android.apk"
	@cd "$(PACKAGE_DIR)" && "$(ZIP)" -qr "$(abspath $@)" "ofd-viewer-android"

package-converter: $(CONVERTER_PACKAGE)

package-validator: $(VALIDATOR_PACKAGE)

package-analyzer: $(ANALYZER_PACKAGE)

$(VALIDATOR_PACKAGE): $(VALIDATOR) cmd/ofd-validator/README.md
	@mkdir -p "$(DIST_DIR)"
	@rm -rf "$(PACKAGE_DIR)/ofd-validator"
	@mkdir -p "$(PACKAGE_DIR)/ofd-validator"
	@rm -f "$@"
	@cp "$(VALIDATOR)" "$(PACKAGE_DIR)/ofd-validator/ofd-validator$(BIN_SUFFIX)"
	@cp "cmd/ofd-validator/README.md" "$(PACKAGE_DIR)/ofd-validator/README.md"
	@cd "$(PACKAGE_DIR)" && "$(ZIP)" -qr "$(abspath $@)" "ofd-validator"

$(ANALYZER_PACKAGE): $(ANALYZER) cmd/ofd-analyzer/README.md
	@mkdir -p "$(DIST_DIR)"
	@rm -rf "$(PACKAGE_DIR)/ofd-analyzer"
	@mkdir -p "$(PACKAGE_DIR)/ofd-analyzer"
	@rm -f "$@"
	@cp "$(ANALYZER)" "$(PACKAGE_DIR)/ofd-analyzer/ofd-analyzer$(BIN_SUFFIX)"
	@cp "cmd/ofd-analyzer/README.md" "$(PACKAGE_DIR)/ofd-analyzer/README.md"
	@cd "$(PACKAGE_DIR)" && "$(ZIP)" -qr "$(abspath $@)" "ofd-analyzer"

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
