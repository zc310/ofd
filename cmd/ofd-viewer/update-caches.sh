#!/bin/sh
# 仅在安装配置完成或移除文件后刷新共享缓存。
case "${1:-}" in
    configure|triggered|remove|purge|[0-9]*) ;;
    *) exit 0 ;;
esac

if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database /usr/share/applications >/dev/null 2>&1 || :
fi
if command -v update-mime-database >/dev/null 2>&1; then
    update-mime-database /usr/share/mime >/dev/null 2>&1 || :
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor >/dev/null 2>&1 || :
fi
exit 0
