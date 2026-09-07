# AnyKernel3

This directory is the AnyKernel3 staging area. forged assembles the flashable
ZIP here after every successful build.

## Where does AnyKernel3 come from?

Set `anykernel3.source` in your build config — forged fetches it for you:

| Mode | What happens |
|---|---|
| `osm0sis` | Clones upstream [osm0sis/AnyKernel3](https://github.com/osm0sis/AnyKernel3) (default) |
| `git` | Clones your own fork — set `anykernel3.repo_url` / `repo_branch` |
| `local` | Copies an existing checkout — set `anykernel3.repo_url` to the path |
| `stub` | Keeps the bare skeleton; ZIPs build but are **not flashable** |

An already-populated real checkout is reused (and `git pull`-refreshed), never
clobbered. forged always renders `anykernel.sh` from your build config; the
upstream `tools/` and `META-INF/` are left untouched.

## CLI override

```bash
forged build -c build_config.toml --anykernel-source git
```

## Layout

```
AnyKernel3/
├── anykernel.sh            # rendered by forged from your config
├── tools/                  # ak3-core.sh + magiskboot (from the source mode)
├── modules/system/lib/     # built .ko files staged here
├── dtb/                    # built DTB files staged here
└── META-INF/...            # updater binaries (upstream's win)
```
