# AnyKernel3

This directory is the AnyKernel3 staging area.

## Setup

Replace `tools/ak3-core.sh` with the real AnyKernel3 core script:

```bash
git clone https://github.com/osm0sis/AnyKernel3 /tmp/ak3
cp /tmp/ak3/tools/ak3-core.sh tools/
cp /tmp/ak3/tools/magiskboot tools/   # if needed
```

The `kernel-builder` tool will populate this directory automatically when
packaging. You only need to provide the real `ak3-core.sh` and any optional
ramdisk overlays or patches.

## Directories

| Path | Purpose |
|------|---------|
| `tools/` | ak3-core.sh, magiskboot, busybox, etc. |
| `modules/` | Kernel modules to push to /system/lib/modules |
| `patch/` | Binary patches applied by AnyKernel3 |
| `ramdisk/` | Ramdisk overlay files |
| `META-INF/` | Flashable ZIP manifest |

## References

- [AnyKernel3 by osm0sis](https://github.com/osm0sis/AnyKernel3)
