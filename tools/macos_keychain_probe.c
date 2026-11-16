#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdio.h>
#include <string.h>

#ifndef PROBE_BUILD_TAG
#define PROBE_BUILD_TAG 0
#endif

static int usage(const char *program) {
    fprintf(stderr, "usage: %s KEYCHAIN SERVICE ACCOUNT\n", program);
    fprintf(stderr, "       %s --build-tag\n", program);
    return 64;
}

int main(int argc, char **argv) {
    if (argc == 2 && strcmp(argv[1], "--build-tag") == 0) {
        printf("%d\n", PROBE_BUILD_TAG);
        return 0;
    }
    if (argc != 4) {
        return usage(argv[0]);
    }

    SecKeychainRef keychain = NULL;
    OSStatus status = SecKeychainOpen(argv[1], &keychain);
    if (status != errSecSuccess) {
        fprintf(stderr, "open keychain: %d\n", (int)status);
        return 65;
    }

    // The integration test must never turn an access denial into an
    // interactive prompt that can hang an unattended run.
    SecKeychainSetUserInteractionAllowed(false);
    UInt32 length = 0;
    void *data = NULL;
    status = SecKeychainFindGenericPassword(
        keychain, (UInt32)strlen(argv[2]), argv[2], (UInt32)strlen(argv[3]),
        argv[3], &length, &data, NULL);
    SecKeychainSetUserInteractionAllowed(true);
    CFRelease(keychain);

    if (status != errSecSuccess) {
        fprintf(stderr, "find generic password: %d\n", (int)status);
        if (status == errSecAuthFailed ||
            status == errSecInteractionNotAllowed) {
            return 77;
        }
        return 66;
    }
    if (length > 0) {
        fwrite(data, 1, length, stdout);
    }
    SecKeychainItemFreeContent(NULL, data);
    return 0;
}
