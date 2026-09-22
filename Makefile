GO ?= go
ZIP ?= zip
FYNE ?= fyne

GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
ARM64_GOOS ?= linux
WINDOWS_CC ?= x86_64-w64-mingw32-gcc
WINDOWS_ARM64_CC ?= aarch64-w64-mingw32-clang
WINDOWS_ARM64_CXX ?= aarch64-w64-mingw32-clang++
CGO_ENABLED ?= 1
ifeq ($(origin CC),default)
CC := $(shell $(GO) env CC)
endif
ifeq ($(origin CXX),default)
CXX := $(shell $(GO) env CXX)
endif
GOHOSTOS := $(shell $(GO) env GOHOSTOS)

ifeq ($(GOOS),windows)
ifeq ($(GOARCH),amd64)
ifeq ($(origin CC),file)
CC := x86_64-w64-mingw32-gcc
endif
endif
ifeq ($(GOARCH),386)
ifeq ($(origin CC),file)
CC := i686-w64-mingw32-gcc
endif
endif
ifeq ($(GOARCH),arm64)
ifeq ($(origin CC),file)
CC := $(WINDOWS_ARM64_CC)
endif
ifeq ($(origin CXX),file)
CXX := $(WINDOWS_ARM64_CXX)
endif
endif
endif

PLATFORM := $(GOOS)-$(GOARCH)
BIN_SUFFIX := $(if $(filter windows,$(GOOS)),.exe,)
GO_LDFLAGS := -s -w
GO_BUILD_FLAGS := -trimpath -ldflags "$(GO_LDFLAGS)"
VIEWER_BUILD_FLAGS := -trimpath -ldflags "$(GO_LDFLAGS) $(if $(filter windows,$(GOOS)),-H=windowsgui,)"
WASM_OPT ?= wasm-opt
WASM_OPT_FLAGS := --enable-bulk-memory --enable-bulk-memory-opt --enable-nontrapping-float-to-int --enable-sign-ext --enable-mutable-globals --enable-simd --enable-reference-types --disable-gc --disable-strings --disable-memory64 --disable-compact-imports -Oz --strip-producers
DIST_DIR := dist
BUILD_DIR := .build
BIN_DIR := $(BUILD_DIR)/bin/$(PLATFORM)
PACKAGE_DIR := $(BUILD_DIR)/packages/$(PLATFORM)

VIEWER := $(BIN_DIR)/ofd-viewer$(BIN_SUFFIX)
CONVERTER := $(BIN_DIR)/ofd-converter$(BIN_SUFFIX)
THUMBNAILER := $(BIN_DIR)/ofd-thumbnailer$(BIN_SUFFIX)
VALIDATOR := $(BIN_DIR)/ofd-validator$(BIN_SUFFIX)
ANALYZER := $(BIN_DIR)/ofd-analyzer$(BIN_SUFFIX)
ARCHIVE := $(BIN_DIR)/ofd-archive$(BIN_SUFFIX)
CREATOR := $(BIN_DIR)/ofd-creator$(BIN_SUFFIX)
SIGNER_DEMO := $(BIN_DIR)/ofd-signer-demo$(BIN_SUFFIX)
WASM := cmd/ofd-wasm/web/ofd.wasm
WASM_EXEC := cmd/ofd-wasm/web/wasm_exec.js
WASM_VIEWER := cmd/ofd-wasm/web/viewer.js
WASM_WORKER := cmd/ofd-wasm/web/worker.js
WASM_INDEX := cmd/ofd-wasm/web/index.html
WASM_ICON_FONT := cmd/ofd-wasm/web/material-symbols-outlined-subset.woff2
WASM_SERVICE_WORKER := cmd/ofd-wasm/web/service-worker.js
WASM_WEB_DIR := cmd/ofd-wasm/web
WASM_WEB_PACKAGE := $(DIST_DIR)/ofd-wasm-web.zip

VIEWER_PACKAGE := $(DIST_DIR)/ofd-viewer-$(PLATFORM).zip
CONVERTER_PACKAGE := $(DIST_DIR)/ofd-converter-$(PLATFORM).zip
THUMBNAILER_PACKAGE := $(DIST_DIR)/ofd-thumbnailer-$(PLATFORM).zip
VALIDATOR_PACKAGE := $(DIST_DIR)/ofd-validator-$(PLATFORM).zip
ANALYZER_PACKAGE := $(DIST_DIR)/ofd-analyzer-$(PLATFORM).zip
ARCHIVE_PACKAGE := $(DIST_DIR)/ofd-archive-$(PLATFORM).zip
CREATOR_PACKAGE := $(DIST_DIR)/ofd-creator-$(PLATFORM).zip
SIGNER_DEMO_PACKAGE := $(DIST_DIR)/ofd-signer-demo-$(PLATFORM).zip
ANDROID_VIEWER_PACKAGE := $(DIST_DIR)/ofd-viewer-android.apk
ANDROID_VIEWER_ZIP := $(DIST_DIR)/ofd-viewer-android.zip
ANDROID_VIEWER_APP_ID := github.com.zc310.ofd.viewer
ANDROID_VIEWER_NAME := OFD Viewer
ANDROID_VIEWER_OUTPUT := OFD_Viewer.apk
VIEWER_VERSION ?= 0.0.5
ANDROID_VIEWER_SOURCES := $(filter-out %_test.go,$(wildcard cmd/ofd-viewer/*.go))

TOOL_BUILD_TARGETS := $(CONVERTER) $(VALIDATOR) $(ANALYZER) $(ARCHIVE) $(CREATOR) $(SIGNER_DEMO) $(if $(filter linux,$(GOOS)),$(THUMBNAILER))
TOOL_PACKAGE_TARGETS := package-converter package-validator package-analyzer package-archive package-creator $(SIGNER_DEMO_PACKAGE) $(if $(filter linux,$(GOOS)),package-thumbnailer)
VIEWER_BUILD_TARGETS := $(if $(or $(and $(filter linux,$(GOOS)),$(filter arm64,$(GOARCH))),$(and $(filter darwin,$(GOOS)),$(filter linux,$(GOHOSTOS)))),,$(VIEWER))
VIEWER_PACKAGE_TARGETS := $(if $(or $(and $(filter linux,$(GOOS)),$(filter arm64,$(GOARCH))),$(and $(filter darwin,$(GOOS)),$(filter linux,$(GOHOSTOS)))),,package-viewer)
WINDOWS_BUILD_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),build-windows-amd64,)
WINDOWS_PACKAGE_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),package-windows-amd64,)
DARWIN_CROSS_BUILD_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),build-darwin-arm64 build-darwin-amd64,)
DARWIN_CROSS_PACKAGE_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),package-darwin-arm64 package-darwin-amd64,)
WINDOWS_ARM64_BUILD_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),build-windows-arm64,)
WINDOWS_ARM64_PACKAGE_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),package-windows-arm64,)
WINDOWS_ARM64_BUILD_CC := $(if $(or $(filter command line,$(origin CC)),$(filter environment%,$(origin CC))),$(CC),$(WINDOWS_ARM64_CC))
WINDOWS_ARM64_BUILD_CXX := $(if $(or $(filter command line,$(origin CXX)),$(filter environment%,$(origin CXX))),$(CXX),$(WINDOWS_ARM64_CXX))
LINUX_ARM64_BUILD_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),build-arm64,)
LINUX_ARM64_PACKAGE_TARGETS := $(if $(and $(filter linux,$(GOHOSTOS)),$(filter linux,$(GOOS))),package-arm64,)
DARWIN_CGO_ENABLED := $(if $(filter linux,$(GOHOSTOS)),0,$(CGO_ENABLED))
DARWIN_BUILD_TARGET := $(if $(filter linux,$(GOHOSTOS)),build-tools,build)
DARWIN_PACKAGE_TARGET := $(if $(filter linux,$(GOHOSTOS)),package-tools,package)

.PHONY: all build build-tools build-wasm build-arm64 build-darwin-arm64 build-darwin-amd64 build-windows-amd64 build-windows-arm64 package package-desktop package-tools package-arm64 package-darwin-arm64 package-darwin-amd64 package-windows-amd64 package-windows-arm64 package-wasm-web package-viewer package-viewer-android package-viewer-android-zip package-converter package-thumbnailer package-validator package-analyzer package-archive package-creator package-signer-demo clean help FORCE

all: package

help:
	@printf '%s\n' \
		'make build                    Build all command programs' \
		'make build-wasm               Build the browser WASM engine' \
		'make package                  Build desktop packages and Android APK ZIP' \
		'make package-wasm-web         Package the browser WASM web directory' \
		'make package-viewer           Build the OFD viewer package' \
		'make package-viewer-android   Build the OFD viewer Android APK' \
		'make package-viewer-android-zip Build the OFD viewer Android ZIP package' \
		'make package-converter       Build the OFD converter package' \
		'make package-validator       Build the OFD validator' \
		'make package-analyzer        Build the OFD analyzer' \
		'make package-archive         Build the OFD archive tool' \
		'make package-creator         Build the OFD creator' \
		'make package-signer-demo     Build the OFD demo signer' \
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
		'  Windows ARM64 uses aarch64-w64-mingw32-clang/clang++ for ofd-viewer' \
		'  CC=aarch64-w64-mingw32-clang CXX=aarch64-w64-mingw32-clang++ make package-windows-arm64' \
		'  Override Windows ARM64 defaults with WINDOWS_ARM64_CC/CXX=...' \
		'  Android is included in the default package target' \
		'  ofd-thumbnailer is built only when GOOS=linux'

build: $(VIEWER_BUILD_TARGETS) $(TOOL_BUILD_TARGETS) $(WINDOWS_BUILD_TARGETS) $(DARWIN_CROSS_BUILD_TARGETS) $(WINDOWS_ARM64_BUILD_TARGETS) $(LINUX_ARM64_BUILD_TARGETS)

build-tools: $(TOOL_BUILD_TARGETS)

# 页面入口、页面脚本、Web Worker、wasm_exec.js 和 ofd.wasm 都使用不带查询参数的固定路径，
# 缓存版本由 CACHE_NAME 的哈希管理：任一资源内容变化，缓存名变化，新 Service
# Worker 安装时删除旧缓存并重新缓存全部资源。
build-wasm: $(WASM) $(WASM_EXEC) $(WASM_SERVICE_WORKER)

$(WASM): FORCE
	@mkdir -p "$(dir $@)"
	@tmp="$@.tmp"; opt="$@.opt"; \
	trap 'rm -f "$$tmp" "$$opt"' EXIT; \
	CGO_ENABLED=0 GOOS=js GOARCH=wasm $(GO) build $(GO_BUILD_FLAGS) -o "$$tmp" ./cmd/ofd-wasm; \
	if command -v "$(WASM_OPT)" >/dev/null 2>&1; then \
		"$(WASM_OPT)" $(WASM_OPT_FLAGS) "$$tmp" -o "$$opt"; \
		mv "$$opt" "$@"; \
	else \
		printf '%s\n' '警告: 未找到 wasm-opt，使用未优化的 WASM。' >&2; \
		mv "$$tmp" "$@"; \
	fi

$(WASM_EXEC): FORCE
	@mkdir -p "$(dir $@)"
	cp "$$(CGO_ENABLED=0 GOOS=js GOARCH=wasm $(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$@"

$(WASM_SERVICE_WORKER): $(WASM_INDEX) $(WASM_VIEWER) $(WASM_WORKER) $(WASM_EXEC) $(WASM) $(WASM_ICON_FONT) FORCE
	@CACHE_NAME=$$(cat "$(WASM_INDEX)" "$(WASM_VIEWER)" "$(WASM_WORKER)" "$(WASM_EXEC)" "$(WASM)" "$(WASM_ICON_FONT)" | sha256sum | cut -c1-16); \
	sed -i "s/^const CACHE_NAME = '.*';$$/const CACHE_NAME = 'ofd-reader-shell_$$CACHE_NAME';/" "$(WASM_SERVICE_WORKER)"

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
	@if ! command -v "$(WINDOWS_ARM64_BUILD_CC)" >/dev/null 2>&1; then echo "错误: 找不到 Windows ARM64 CGO 编译器 $(WINDOWS_ARM64_BUILD_CC)，请安装 LLVM MinGW 或通过 CC/WINDOWS_ARM64_CC 指定编译器。" >&2; exit 1; fi
	@if ! command -v "$(WINDOWS_ARM64_BUILD_CXX)" >/dev/null 2>&1; then echo "错误: 找不到 Windows ARM64 C++ 编译器 $(WINDOWS_ARM64_BUILD_CXX)，请安装 LLVM MinGW 或通过 CXX/WINDOWS_ARM64_CXX 指定编译器。" >&2; exit 1; fi
	$(MAKE) GOOS=windows GOARCH=arm64 CGO_ENABLED=1 CC=$(WINDOWS_ARM64_BUILD_CC) CXX=$(WINDOWS_ARM64_BUILD_CXX) build

$(VIEWER): FORCE
	@mkdir -p "$(BIN_DIR)"
	@if [ "$(GOOS)" = "windows" ] && ! command -v "$(CC)" >/dev/null 2>&1; then echo "错误: 找不到 Windows CGO 编译器 $(CC)，请安装 MinGW-w64 或通过 CC 指定编译器。" >&2; exit 1; fi
	CC=$(CC) CXX=$(CXX) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(VIEWER_BUILD_FLAGS) -o "$@" ./cmd/ofd-viewer

$(CONVERTER): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GO_BUILD_FLAGS) -o "$@" ./cmd/ofd-converter

$(VALIDATOR): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GO_BUILD_FLAGS) -o "$@" ./cmd/ofd-validator

$(ANALYZER): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GO_BUILD_FLAGS) -o "$@" ./cmd/ofd-analyzer

$(ARCHIVE): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GO_BUILD_FLAGS) -o "$@" ./cmd/ofd-archive

$(CREATOR): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GO_BUILD_FLAGS) -o "$@" ./cmd/ofd-creator

$(SIGNER_DEMO): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GO_BUILD_FLAGS) -o "$@" ./cmd/ofd-signer-demo

ifeq ($(GOOS),linux)
$(THUMBNAILER): FORCE
	@mkdir -p "$(BIN_DIR)"
	CC=$(CC) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GO_BUILD_FLAGS) -o "$@" ./cmd/ofd-thumbnailer
endif

package: package-desktop package-viewer-android-zip package-wasm-web $(WINDOWS_PACKAGE_TARGETS) $(DARWIN_CROSS_PACKAGE_TARGETS) $(WINDOWS_ARM64_PACKAGE_TARGETS) $(LINUX_ARM64_PACKAGE_TARGETS)

package-desktop: $(VIEWER_PACKAGE_TARGETS) $(TOOL_PACKAGE_TARGETS)

package-tools: $(TOOL_PACKAGE_TARGETS)

package-wasm-web: $(WASM_WEB_PACKAGE)

$(WASM_WEB_PACKAGE): build-wasm FORCE
	@mkdir -p "$(DIST_DIR)"
	@rm -f "$@"
	@cd "$(dir $(WASM_WEB_DIR))" && "$(ZIP)" -qr "$(abspath $@)" "$(notdir $(WASM_WEB_DIR))"

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
	@if ! command -v "$(WINDOWS_ARM64_BUILD_CC)" >/dev/null 2>&1; then echo "错误: 找不到 Windows ARM64 CGO 编译器 $(WINDOWS_ARM64_BUILD_CC)，请安装 LLVM MinGW 或通过 CC/WINDOWS_ARM64_CC 指定编译器。" >&2; exit 1; fi
	@if ! command -v "$(WINDOWS_ARM64_BUILD_CXX)" >/dev/null 2>&1; then echo "错误: 找不到 Windows ARM64 C++ 编译器 $(WINDOWS_ARM64_BUILD_CXX)，请安装 LLVM MinGW 或通过 CXX/WINDOWS_ARM64_CXX 指定编译器。" >&2; exit 1; fi
	$(MAKE) GOOS=windows GOARCH=arm64 CGO_ENABLED=1 CC=$(WINDOWS_ARM64_BUILD_CC) CXX=$(WINDOWS_ARM64_BUILD_CXX) package-desktop

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

package-archive: $(ARCHIVE_PACKAGE)

package-creator: $(CREATOR_PACKAGE)

package-signer-demo: $(SIGNER_DEMO_PACKAGE)

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

$(ARCHIVE_PACKAGE): $(ARCHIVE) cmd/ofd-archive/README.md
	@mkdir -p "$(DIST_DIR)"
	@rm -rf "$(PACKAGE_DIR)/ofd-archive"
	@mkdir -p "$(PACKAGE_DIR)/ofd-archive"
	@rm -f "$@"
	@cp "$(ARCHIVE)" "$(PACKAGE_DIR)/ofd-archive/ofd-archive$(BIN_SUFFIX)"
	@cp "cmd/ofd-archive/README.md" "$(PACKAGE_DIR)/ofd-archive/README.md"
	@cd "$(PACKAGE_DIR)" && "$(ZIP)" -qr "$(abspath $@)" "ofd-archive"

$(CREATOR_PACKAGE): $(CREATOR) cmd/ofd-creator/README.md
	@mkdir -p "$(DIST_DIR)"
	@rm -rf "$(PACKAGE_DIR)/ofd-creator"
	@mkdir -p "$(PACKAGE_DIR)/ofd-creator"
	@rm -f "$@"
	@cp "$(CREATOR)" "$(PACKAGE_DIR)/ofd-creator/ofd-creator$(BIN_SUFFIX)"
	@cp "cmd/ofd-creator/README.md" "$(PACKAGE_DIR)/ofd-creator/README.md"
	@cd "$(PACKAGE_DIR)" && "$(ZIP)" -qr "$(abspath $@)" "ofd-creator"

$(SIGNER_DEMO_PACKAGE): $(SIGNER_DEMO) cmd/ofd-signer-demo/README.md
	@mkdir -p "$(DIST_DIR)"
	@rm -rf "$(PACKAGE_DIR)/ofd-signer-demo"
	@mkdir -p "$(PACKAGE_DIR)/ofd-signer-demo"
	@rm -f "$@"
	@cp "$(SIGNER_DEMO)" "$(PACKAGE_DIR)/ofd-signer-demo/ofd-signer-demo$(BIN_SUFFIX)"
	@cp "cmd/ofd-signer-demo/README.md" "$(PACKAGE_DIR)/ofd-signer-demo/README.md"
	@cd "$(PACKAGE_DIR)" && "$(ZIP)" -qr "$(abspath $@)" "ofd-signer-demo"

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
	@rm -rf "$(BUILD_DIR)" "$(DIST_DIR)" "$(WASM)" "$(WASM_EXEC)"

FORCE:
