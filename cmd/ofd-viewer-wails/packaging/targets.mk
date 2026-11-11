WAILS_BINARY = $(BIN_DIR)/ofd-viewer-wails$(BIN_SUFFIX)
WAILS_PACKAGE = $(DIST_DIR)/ofd-viewer-wails-$(PLATFORM).zip
MAKENSIS ?= makensis
WAILS_WINRES ?= $(GO) run github.com/tc-hib/go-winres@v0.3.3
WAILS_PYTHON ?= python3
WAILS_ANDROID_ARCH ?= arm64
WAILS_ANDROID_VERSION_CODE ?= 1
WAILS_ANDROID_RELEASE ?= 0

.PHONY: build-wails-android package-wails-android bundle-wails-android
build-wails-android package-wails-android bundle-wails-android:
	$(WAILS_PYTHON) cmd/ofd-viewer-wails/packaging/android/build.py --go "$(GO)" --version "$(VERSION)" --version-code "$(WAILS_ANDROID_VERSION_CODE)" --arch "$(WAILS_ANDROID_ARCH)" --build-dir "$(BUILD_DIR)/wails-android" --out "$(DIST_DIR)" --format $(if $(filter package-wails-android,$@),apk,$(if $(filter bundle-wails-android,$@),aab,native)) $(if $(filter 1,$(WAILS_ANDROID_RELEASE)),--release,)

# ICO 是生成产物；仅 Windows 构建需要，源图或脚本变化时重建。
cmd/ofd-viewer-wails/packaging/icon.ico: cmd/ofd-viewer/share/io.github.zc310.ofd.svg cmd/ofd-viewer-wails/packaging/generate_icon.py
	$(WAILS_PYTHON) cmd/ofd-viewer-wails/packaging/generate_icon.py

ifeq ($(GOOS),windows)
build-wails: cmd/ofd-viewer-wails/packaging/icon.ico
endif

.PHONY: build-wails package-wails package-wails-deb package-wails-rpm package-wails-linux package-wails-installer

build-wails:
	@mkdir -p "$(BIN_DIR)"
	@if [ "$(GOOS)" = darwin ] && [ "$(GOHOSTOS)" != darwin ]; then echo "错误: Wails macOS 应用需在 macOS 上构建。" >&2; exit 1; fi
	CC=$(CC) CXX=$(CXX) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(VIEWER_BUILD_FLAGS) -o "$(WAILS_BINARY)" ./cmd/ofd-viewer-wails
ifeq ($(GOOS),windows)
	GOOS=$(GOHOSTOS) GOARCH=$(shell $(GO) env GOHOSTARCH) CGO_ENABLED=0 $(WAILS_WINRES) patch --in cmd/ofd-viewer-wails/packaging/windows-resources.json --no-backup "$(WAILS_BINARY)"
endif

package-wails: build-wails
	$(WAILS_PYTHON) cmd/ofd-viewer-wails/packaging/package.py --platform "$(GOOS)" --binary "$(abspath $(WAILS_BINARY))" --output "$(abspath $(WAILS_PACKAGE))" --version "$(VERSION)"

package-wails-linux: package-wails-deb package-wails-rpm

package-wails-deb package-wails-rpm: build-wails
	@test "$(GOOS)" = linux || { echo "错误: DEB/RPM 仅支持 Linux。" >&2; exit 1; }
	@command -v "$(NFPM)" >/dev/null || { echo "错误: 请安装 nfpm，或设置 NFPM。" >&2; exit 1; }
	@mkdir -p "$(DIST_DIR)"
	@glibc_version=$$(LC_ALL=C "$(READELF)" --version-info "$(WAILS_BINARY)" | sed -n 's/.*Name: GLIBC_\([0-9.]*\).*/\1/p' | sort -V | tail -n 1); \
	test -n "$$glibc_version" || { echo "错误: 无法确定 glibc 依赖；系统包必须启用 CGO。" >&2; exit 1; }; \
	WAILS_GLIBC_VERSION="$$glibc_version" WAILS_VERSION="$(VERSION)" WAILS_ARCH="$(GOARCH)" WAILS_BINARY="$(abspath $(WAILS_BINARY))" \
	"$(NFPM)" package --config cmd/ofd-viewer-wails/packaging/nfpm.yaml --packager $(patsubst package-wails-%,%,$@) --target "$(abspath $(DIST_DIR))/"

package-wails-installer: build-wails
	@test "$(GOOS)" = windows || { echo "错误: NSIS 安装包需要 GOOS=windows。" >&2; exit 1; }
	@mkdir -p "$(DIST_DIR)"
	$(MAKENSIS) -DVERSION="$(VERSION)" -DBINARY="$(abspath $(WAILS_BINARY))" -DOUTPUT="$(abspath $(DIST_DIR))/ofd-viewer-wails-$(PLATFORM)-setup.exe" cmd/ofd-viewer-wails/packaging/windows.nsi
