"""Allow platformio build_flags to override WebSockets frame size.

The upstream header unconditionally #defines WEBSOCKETS_MAX_DATA_SIZE to 15KB,
which overwrites -D WEBSOCKETS_MAX_DATA_SIZE=... from build_flags.
"""

import os

Import("env")  # type: ignore[name-defined]  # PlatformIO SCons


def patch_websockets_header() -> None:
    path = os.path.join(
        env["PROJECT_DIR"],
        ".pio",
        "libdeps",
        env["PIOENV"],
        "WebSockets",
        "src",
        "WebSockets.h",
    )
    if not os.path.isfile(path):
        return

    with open(path, encoding="utf-8") as fh:
        text = fh.read()

    needle = "#define WEBSOCKETS_MAX_DATA_SIZE (15 * 1024)"
    replacement = (
        "#ifndef WEBSOCKETS_MAX_DATA_SIZE\n"
        "#define WEBSOCKETS_MAX_DATA_SIZE (15 * 1024)\n"
        "#endif"
    )
    if needle not in text or replacement in text:
        return

    text = text.replace(needle, replacement)
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(text)
    print(f"[patch] {path}: WEBSOCKETS_MAX_DATA_SIZE is now overridable")


patch_websockets_header()
