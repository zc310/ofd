#!/usr/bin/env bash
# 用共享镜像中的工具链构建，以 runner 身份写入工作区、缓存和发布目录。
set -euo pipefail

: "${GITHUB_WORKSPACE:?请设置 GITHUB_WORKSPACE 为仓库根目录}"
: "${HOME:?请设置 HOME}"
image="${OFD_CI_IMAGE:-ofd-ci:local}"
output_dir="${OFD_CI_OUTPUT_DIR:-/tmp/ofd}"
mkdir -p "$HOME/go" "$HOME/.cache/go-build" "$output_dir"

if [[ "$#" -eq 0 ]]; then
    set -- bash
fi

exec docker run --rm --init \
    --user "$(id -u):$(id -g)" \
    --volume "$HOME:$HOME" \
    --volume "$GITHUB_WORKSPACE:$GITHUB_WORKSPACE" \
    --volume "$output_dir:/tmp/ofd" \
    --volume /etc/passwd:/etc/passwd:ro \
    --volume /etc/group:/etc/group:ro \
    --workdir "$GITHUB_WORKSPACE" \
    --env HOME \
    --env GOPATH="$HOME/go" \
    --env GOCACHE="$HOME/.cache/go-build" \
    --env GOTOOLCHAIN=local \
    --env EGL_PLATFORM=surfaceless \
    --env RELEASE_TAG \
    "$image" bash -e -o pipefail -c '
        # 使用镜像的 GOROOT；只增加持久化的 Go 工具目录，不挂载宿主机工具链。
        export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"
        exec "$@"
    ' ofd-ci "$@"
