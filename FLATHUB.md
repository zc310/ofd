# Flathub Build

This repository contains a Flatpak manifest for OFD Viewer:

```bash
flatpak install flathub org.flatpak.Builder
flatpak run org.flatpak.Builder --user --install --force-clean --disable-rofiles-fuse build-dir io.github.zc310.ofd.json
flatpak run io.github.zc310.ofd
```

The Go dependency sources in `go.mod.json` and `modules.txt` are generated with:

```bash
go run github.com/dennwc/flatpak-go-mod@latest -json .
```

The manifest is pinned to the current source commit while the `v0.0.5` release is prepared. Before submitting to Flathub, replace the manifest commit with the immutable commit for the released `v0.0.5` tag and verify it with `flatpak-builder --check-metadata` and `appstreamcli validate`.

The application ID is `io.github.zc310.ofd` and must remain identical in the Go application, manifest, desktop file, icon, and AppStream metadata.

The application uses Fyne's file dialog. The manifest builds with the `flatpak` build tag so Fyne uses the XDG Desktop Portal file chooser inside the sandbox.

The desktop entry uses Flatpak file forwarding so OFD files opened from a file manager are exposed to the sandbox.

The ID follows Flathub's GitHub application ID convention for `https://github.com/zc310/ofd`.
