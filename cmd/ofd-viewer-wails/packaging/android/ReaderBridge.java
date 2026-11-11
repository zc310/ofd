package com.wails.app;

import android.app.Activity;
import android.content.Intent;
import android.database.Cursor;
import android.net.Uri;
import android.os.Handler;
import android.os.Looper;
import android.os.ParcelFileDescriptor;
import android.provider.OpenableColumns;
import android.util.Base64;
import android.webkit.MimeTypeMap;
import java.nio.charset.StandardCharsets;
import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.util.Locale;

/** 阅读器专用 SAF 桥：不向网页暴露 URI、文件描述符或通用文件系统访问。 */
public final class ReaderBridge {
    private static final int SAVE_REQUEST_START = 20000;
    private int saveSequence;
    private volatile int pendingSaveRequest = -1;
    static { System.loadLibrary("wails"); }
    private final Activity activity;
    private final Handler handler = new Handler(Looper.getMainLooper());
    private volatile String status = "cancel";
    private volatile Uri target;
    private native void install();
    public native void dispose();

    public ReaderBridge(Activity activity) {
        this.activity = activity;
        install();
    }

    public synchronized String saveStart(String encoded) {
        String name = new String(Base64.decode(encoded, Base64.DEFAULT), StandardCharsets.UTF_8);
        target = null;
        status = "pending";
        final int request = SAVE_REQUEST_START + (saveSequence++ % 10000);
        pendingSaveRequest = request;
        handler.post(() -> {
            try {
                Intent intent = new Intent(Intent.ACTION_CREATE_DOCUMENT);
                intent.addCategory(Intent.CATEGORY_OPENABLE);
                String ext = name.contains(".") ? name.substring(name.lastIndexOf('.') + 1).toLowerCase(Locale.ROOT) : "";
                String mime = MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext);
                intent.setType(mime == null ? "application/octet-stream" : mime);
                intent.putExtra(Intent.EXTRA_TITLE, name);
                synchronized (this) {
                    if (pendingSaveRequest == request) activity.startActivityForResult(intent, request);
                }
            } catch (Exception e) {
                synchronized (this) { if (pendingSaveRequest == request) status = "error"; }
            }
        });
        return "pending";
    }

    public synchronized boolean onActivityResult(int request, int result, Intent data) {
        if (request < SAVE_REQUEST_START || request >= SAVE_REQUEST_START + 10000) return false;
        if (request != pendingSaveRequest || !status.equals("pending")) return true;
        Uri uri = result == Activity.RESULT_OK && data != null ? data.getData() : null;
        // 只接受系统授予的 content URI，不能写入任意 file:// 路径。
        if (uri != null && "content".equals(uri.getScheme())) {
            target = uri;
            status = "ready";
        } else { status = uri == null ? "cancel" : "error"; }
        return true;
    }

    public synchronized String savePoll(String ignored) { return status; }

    public String saveOpen(String ignored) {
        Uri uri;
        synchronized (this) {
            uri = target;
            target = null;
            if (!"ready".equals(status) || uri == null) return "error";
            status = "writing";
        }
        try (ParcelFileDescriptor file = activity.getContentResolver().openFileDescriptor(uri, "wt")) {
            if (file == null) return "error";
            return Integer.toString(file.detachFd());
        } catch (Exception e) { return "error"; }
    }

    public synchronized String saveRelease(String ignored) {
        final int request = pendingSaveRequest;
        pendingSaveRequest = -1;
        target = null;
        status = "cancel";
        if (request != -1) handler.post(() -> activity.finishActivity(request));
        return "cancel";
    }

    /** 有界导入，文件名不能逃逸独立的私有临时目录。 */
    public String importDocument(Uri uri) {
        if (uri == null || !"content".equals(uri.getScheme())) return null;
        String name = "document.ofd";
        try (Cursor cursor = activity.getContentResolver().query(uri,
                new String[]{OpenableColumns.DISPLAY_NAME}, null, null, null)) {
            if (cursor != null && cursor.moveToFirst() && cursor.getString(0) != null)
                name = cursor.getString(0).replace('\\', '/');
        } catch (Exception ignored) { }
        name = new File(name).getName();
        if (name.isEmpty() || name.equals(".") || name.equals("..")) name = "document.ofd";
        File parent = new File(activity.getFilesDir(), "imports");
        File dir = new File(parent, java.util.UUID.randomUUID().toString());
        File out = new File(dir, name);
        boolean complete = false;
        try {
            if (!dir.mkdirs()) return null;
            try (InputStream input = activity.getContentResolver().openInputStream(uri);
                 FileOutputStream output = new FileOutputStream(out)) {
                if (input == null) return null;
                byte[] buffer = new byte[64 * 1024];
                long total = 0;
                int count;
                while ((count = input.read(buffer)) != -1) {
                    total += count;
                    if (total > (512L << 20)) throw new java.io.IOException("OFD import exceeds 512 MiB");
                    output.write(buffer, 0, count);
                }
            }
            complete = true;
            return out.getAbsolutePath();
        } catch (Exception e) {
            handler.post(() -> android.widget.Toast.makeText(activity,
                "文件导入失败（读取错误或超过 512 MiB）", android.widget.Toast.LENGTH_LONG).show());
            return null;
        } finally {
            if (!complete) { out.delete(); dir.delete(); }
        }
    }
}
