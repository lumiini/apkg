# apkg

[![Go](https://img.shields.io/badge/go-≥1.21-blue?logo=go)](https://go.dev/) [![License: MPL-2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](https://opensource.org/licenses/MPL-2.0)
 [![Build Status](https://img.shields.io/badge/build-passing-brightgreen)](#)

A *nicely-ish vibecoded* package manager that uses **apk** repos. Or a crappy version of ``apk``

> ⚠ **Warning:**  
> Do **NOT** uninstall packages unless you *really* know what you're doing —  
> otherwise you’ll probably end up with a broken system! (it's kinda crappy)

---

> **Note:**  
> The dependency resolution is highly experimental and partially broken (don’t use it! `maybe`)

---

## Building

1. Clone the repo
2. CD into the directory
3. Install Go  
4. Run:  

```bash
go build .
```

Or, for a statically linked binary (called apkg):

```bash
CGO_ENABLED=0 go build -ldflags="-s -w -extldflags '-static'" -o apkg
```
## Configuration

* Configuration is written in YAML, the file must be called `apkg.yaml`, and either be in the working directory, with the binary or specified with the `-config` flag

You can have one or more repositories (only works with apk v2, not apk v3):
```yaml
repos:
  - https://dl-cdn.alpinelinux.org/alpine/v3.22/main/x86_64
  - https://dl-cdn.alpinelinux.org/alpine/v3.22/community/x86_64
```
Packages are defined similarly:
```yaml
packages:
  - busybox
  - uutils-coreutils
```
Other toggles must all be set before using apkg — otherwise it will (probably) break:
```yaml
# If set to "false" packages will only be staged but not merged into the system
install: true

# Sets the directory to install to.
# Set to "/" if you want to actually install the packages to the system.
# If set to a directory like "test-root", it will merge all changes into that folder instead of root.
install_dir: test-root

# Whether to run pre-, mid-, or post-install scripts
run_scripts: false

# Whether to use the crappy dependency resolution (not recommended)
resolve_deps: false
```

# Usage
```bash
apkg [flags]                  # Install/upgrade/uninstall to match config
apkg add <pkg>                # Add a package to the config and install it
apkg remove | del <pkg>       # Remove a package from the config and uninstall it
apkg reinstall <pkg>          # Force reinstall a package
apkg regen-indexes            # Regenerate installed file indexes
apkg list-installed           # List installed packages and versions
apkg help                     # Print this help message

Flags:

-config <file>   Path to config file (default: apkg.yaml)
-dry-run         Show what would be done, but doesen't modify anything 🔴 IS BROKEN AND DOES MODIFY, DO NOT TRUST 🔴
-v               Enable verbose output
-h, --help       Print a shorter version of this help message
```
Installed packages are automatically indexed in a file called `installed.yaml` after being installed, it will look something like this:

``installed.yaml``
```yaml
- name: busybox
  version: 1.37.0-r19
- name: uutils-coreutils
  version: 0.1.0-r0
```

## Indexing & Uninstallation ⚠*WIP*⚠

At install time packages are indexed, with the files that that package contains being put in a folder called installed_files, in the same directory as the binary, it contains the files of each package in yaml format e.g.:
``busybox.yaml``
```yaml
- bin/busybox
- etc/busybox-paths.d/busybox
- etc/logrotate.d/acpid
- etc/network/if-up.d/dad
- etc/securetty
- etc/udhcpc/udhcpc.conf
- usr/share/udhcpc/default.script
```
or
``htop.yaml``
```yaml
- usr/bin/htop
- usr/share/applications/htop.desktop
- usr/share/icons/hicolor/128x128/apps/htop.png
- usr/share/icons/hicolor/scalable/apps/htop.svg
```
---

Packages can be reindexed by running ```apkg regen-indexes```, if you for example, delete the folder.

The indexing is necessary due to the improper nature of this tool's uninstall mechanism, which just deletes every file that it indexed for that package, when it is uninstalled `Currently it doesen't delete the folders but this will be fixed soon™`
