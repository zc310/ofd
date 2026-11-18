FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    g++ \
    gcc \
    git \
    make \
    nodejs \
    pkg-config \
    python3 \
    libgl1-mesa-dev \
    xorg-dev \
    libwayland-dev \
    libx11-dev \
    libx11-xcb-dev \
    libxkbcommon-x11-dev \
    libxkbcommon-dev \
    libgles2-mesa-dev \
    libegl1-mesa-dev \
    libgtk-4-dev \
    libwebkitgtk-6.0-dev \
    libffi-dev \
    libxcursor-dev \
    libvulkan-dev \
    poppler-utils \
    fontconfig \
    fonts-wqy-zenhei \
    fonts-noto-cjk \
    unzip \
    zip \
    && rm -rf /var/lib/apt/lists/*

RUN set -eu; \
    fonts_dir=/usr/local/share/fonts/hanzi-fonts; \
    download_dir="$(mktemp -d)"; \
    base_url=https://github.com/zispace/hanzi-fonts/releases/download/v1.0; \
    for archive in doc-foundertype.zip doc-zhongyi-etc.zip latin-fonts.zip; do \
        curl --fail --location --silent --show-error \
            --retry 3 --retry-all-errors --connect-timeout 20 --max-time 300 \
            "$base_url/$archive" --output "$download_dir/$archive"; \
        mkdir -p "$fonts_dir/${archive%.zip}"; \
        unzip -q "$download_dir/$archive" -d "$fonts_dir/${archive%.zip}"; \
    done; \
    rm -rf "$download_dir"; \
    # 字体包可能带有 0700 目录，确保非 root 测试用户也能扫描和读取字体。 \
    chmod -R a+rX "$fonts_dir"; \
    fc-cache -f "$fonts_dir"

# Ubuntu 软件源中的版本不支持 Makefile 使用的 wasm-opt 选项。
RUN set -eu; \
    version=133; \
    base="https://github.com/WebAssembly/binaryen/releases/download/version_${version}"; \
    file="binaryen-version_${version}-x86_64-linux.tar.gz"; \
    download_dir="$(mktemp -d)"; \
    curl -sSL --fail --max-time 600 -o "$download_dir/$file" "$base/$file"; \
    curl -sSL --fail --max-time 60 -o "$download_dir/$file.sha256" "$base/$file.sha256"; \
    (cd "$download_dir" && sha256sum -c "$file.sha256"); \
    tar -xzf "$download_dir/$file" -C "$download_dir"; \
    install -m755 "$download_dir/binaryen-version_${version}/bin/wasm-opt" /usr/local/bin/wasm-opt; \
    wasm-opt --enable-bulk-memory-opt --disable-strings --disable-compact-imports --version; \
    rm -rf "$download_dir"
