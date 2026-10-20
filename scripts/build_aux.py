#!/usr/bin/env python3
"""build.bat 辅助：整理 mipsle 产物名 + sha256 + 生成 local-apps manifest。

用法: py -3 scripts/build_aux.py <elf> <manifest-out>

不修改 ELF e_flags。GOARCH=mipsle 产物天然是 MIPS32r1 标记，与 XBurst r2
CPU 向下兼容；设备约定（c1slim-toolkit docs/device-quirks.md）明确要求保留
编译器生成的 ELF 标记，禁止改头伪装 ISA。
"""
import hashlib
import json
import shutil
import sys

APP_ID = "weread"
APP_NAME = "微信读书"
EXECUTABLE = "/storage/c1/local-apps/apps/weread/bin/c1weread"


def main() -> int:
    elf_in, manifest_out = sys.argv[1:3]
    data = open(elf_in, "rb").read()
    if data[:4] != b"\x7fELF":
        raise SystemExit("not an ELF: %s" % elf_in)
    # e_ident[EI_DATA]=1 小端；e_machine=8 MIPS；e_ident[EI_CLASS]=1 o32
    if data[5] != 1 or data[4] != 1:
        raise SystemExit("want ELF32 little-endian, got class=%d data=%d" % (data[4], data[5]))
    if int.from_bytes(data[18:20], "little") != 8:
        raise SystemExit("want e_machine EM_MIPS(8)")

    elf_out = elf_in[:-4] if elf_in.endswith(".exe") else elf_in + ".final"
    shutil.copyfile(elf_in, elf_out)
    digest = hashlib.sha256(data).hexdigest()
    manifest = {
        "id": APP_ID,
        "name": APP_NAME,
        "executable": EXECUTABLE,
        "sha256": digest,
    }
    with open(manifest_out, "w", encoding="utf-8") as fh:
        fh.write(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    print("final %s -> %s" % (elf_in, elf_out))
    print("e_flags = 0x%08x (kept as-is)" % int.from_bytes(data[36:40], "little"))
    print("sha256 = %s" % digest)
    print("manifest -> %s" % manifest_out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
