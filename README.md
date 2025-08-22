# apkg

[![Go](https://img.shields.io/badge/go-≥1.21-blue?logo=go)](https://go.dev/) [![License: MPL-2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](https://opensource.org/licenses/MPL-2.0)
 [![Build Status](https://img.shields.io/badge/build-passing-brightgreen)](#)

A *nicely-ish vibecoded* package manager that uses **apk** repos.  

> ⚠ **Warning:**  
> Do **NOT** uninstall system packages unless you *really* know what you're doing —  
> otherwise you’ll probably end up with a broken system! (it's kinda crappy)

---

> **Note:**  
> The dependency resolution is highly experimental and partially broken (don’t use it!)

---

## Building

1. Clone the repo  
2. Install Go  
3. Run:  

```bash
go build .

Or, for a statically linked binary (called apkg):

CGO_ENABLED=0 go build -ldflags="-s -w -extldflags '-static'" -o apkg
```
## Configuration

* Configuration is written in YAML.

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
-dry-run         Show what would be done, but don't modify anything
-v               Enable verbose output
-h, --help       Print a shorter version of this help message
```
