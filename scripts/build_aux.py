#!/usr/bin/env python3
"""build.bat 辅助：ELF r1->r2 补丁 + sha256 + 生成 local-apps manifest。

用法: py -3 scripts/build_aux.py <elf> <manifest-out>
"""
import hashlib
import json
import struct
import sys

APP_ID = "weread"
APP_NAME = "微信读书"
EXECUTABLE = "/storage/c1/local-apps/apps/weread/bin/c1weread"


def patch_elf_r2(path: str) -> bytes:
    """读 ELF，把 e_flags 的 ISA 域（高 nibble，偏移 36）从 r1(0x5) 改 r2(0x7)。"""
    data = bytearray(open(path, "rb").read())
    flags = struct.unpack("<I", data[36:40])[0]
    if flags & 0xF0000000 != 0x50000000:
        raise SystemExit("unexpected e_flags ISA nibble: 0x%x" % (flags >> 28))
    data[36:40] = struct.pack("<I", (flags & ~0xF0000000) | 0x70000000)
    return bytes(data)


def main() -> int:
    elf_in, manifest_out = sys.argv[1:3]
    patched = patch_elf_r2(elf_in)
    elf_out = elf_in[:-4] if elf_in.endswith(".exe") else elf_in + ".patched"
    open(elf_out, "wb").write(patched)
    digest = hashlib.sha256(patched).hexdigest()
    manifest = {
        "id": APP_ID,
        "name": APP_NAME,
        "executable": EXECUTABLE,
        "sha256": digest,
    }
    with open(manifest_out, "w", encoding="utf-8") as fh:
        fh.write(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    print("patched %s -> %s" % (elf_in, elf_out))
    print("sha256 = %s" % digest)
    print("manifest -> %s" % manifest_out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
