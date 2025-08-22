# apkg

A *nicely-ish vibecoded* package manager that uses **apk** repos.  

> ⚠ **Warning:**  
> Do **NOT** uninstall system packages unless you *really* know what you're doing —  
> otherwise you’ll probably end up with a broken system! (it's kinda crappy)
---

> **Note**
> The dependancy resolution is highly experimental, and partially broken (Don't use it!)

## Building
--
Clone the repo
Install Go
Run ```go build . ´´´ or ```CGO_ENABLED=0 go build -ldflags="-s -w -extldflags '-static'" -o apkg ``` for a statically linked binary, called apkg

---

## Configuration
### The config is written in yaml,
You can have one or more repositories (Only works with apk v2 ones, not apk v3)
```yaml
repos:
    - https://dl-cdn.alpinelinux.org/alpine/v3.22/main/x86_64
    - https://dl-cdn.alpinelinux.org/alpine/v3.22/community/x86_64
```
Packages, the same as repos eg:
```yaml
packages:
    - busybox
    - uutils-coreutils
```
The other toggles must all be set before using apkg or it will (probobly) break
```yaml
#/ If set to "false" packages will only be staged but not merged into the system
install: true
#/ Sets the directory to install to, set to / if you want to acually install the packages to the system, if it is set
#/ To a directory like ``test-root`` then it will merge all changes into that folder instead of root
install_dir: test-root
#/ Wether to run pre-, mid-, or post-install scripts
run_scripts: false
#/ Weather to use the crappy dependancy resolution (Not recomended to be true)
resolve_deps: false
```

---

## Usage

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

