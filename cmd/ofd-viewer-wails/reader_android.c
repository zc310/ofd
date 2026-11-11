// 项目专用 JNI 桥，不替换 Wails 自身的初始化和生命周期符号。
#include <jni.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

static JavaVM *readerVM;
static jobject readerBridge;
static pthread_mutex_t readerLock = PTHREAD_MUTEX_INITIALIZER;

JNIEXPORT void JNICALL Java_com_wails_app_ReaderBridge_install(JNIEnv *env, jobject self) {
    pthread_mutex_lock(&readerLock);
    (*env)->GetJavaVM(env, &readerVM);
    if (readerBridge) (*env)->DeleteGlobalRef(env, readerBridge);
    readerBridge = (*env)->NewGlobalRef(env, self);
    pthread_mutex_unlock(&readerLock);
}

JNIEXPORT void JNICALL Java_com_wails_app_ReaderBridge_dispose(JNIEnv *env, jobject self) {
    pthread_mutex_lock(&readerLock);
    if (readerBridge && (*env)->IsSameObject(env, readerBridge, self)) {
        (*env)->DeleteGlobalRef(env, readerBridge);
        readerBridge = NULL;
    }
    pthread_mutex_unlock(&readerLock);
}

char *ofdReaderCall(const char *method, const char *argument) {
    char *result = NULL;
    pthread_mutex_lock(&readerLock);
    if (!readerVM || !readerBridge) goto done;
    JNIEnv *env = NULL;
    int attached = 0;
    if ((*readerVM)->GetEnv(readerVM, (void **)&env, JNI_VERSION_1_6) == JNI_EDETACHED) {
        if ((*readerVM)->AttachCurrentThread(readerVM, &env, NULL) != JNI_OK) goto done;
        attached = 1;
    }
    if (!env) goto done;
    jobject bridge = (*env)->NewLocalRef(env, readerBridge);
    // Java 的 SAF Provider 可能阻塞，不能持有全局锁阻塞 Activity 销毁。
    pthread_mutex_unlock(&readerLock);
    jclass cls = bridge ? (*env)->GetObjectClass(env, bridge) : NULL;
    jmethodID mid = cls ? (*env)->GetMethodID(env, cls, method, "(Ljava/lang/String;)Ljava/lang/String;") : NULL;
    if (mid) {
        jstring arg = (*env)->NewStringUTF(env, argument);
        jstring value = (jstring)(*env)->CallObjectMethod(env, bridge, mid, arg);
        if (!(*env)->ExceptionCheck(env) && value) {
            const char *text = (*env)->GetStringUTFChars(env, value, NULL);
            if (text) {
                result = strdup(text);
                (*env)->ReleaseStringUTFChars(env, value, text);
            }
        }
        if (value) (*env)->DeleteLocalRef(env, value);
        if (arg) (*env)->DeleteLocalRef(env, arg);
    }
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
    if (cls) (*env)->DeleteLocalRef(env, cls);
    if (bridge) (*env)->DeleteLocalRef(env, bridge);
    if (attached) (*readerVM)->DetachCurrentThread(readerVM);
    return result;
done:
    pthread_mutex_unlock(&readerLock);
    return result;
}
