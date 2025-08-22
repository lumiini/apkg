/* This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/. */

/* Copyright (c) 2025 Lumiini */

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
)

// Config represents the structure of apkg.yaml
type Config struct {
	Repos       []string `yaml:"repos"`
	Packages    []string `yaml:"packages"`
	Install     bool     `yaml:"install"`
	InstallDir  string   `yaml:"install_dir"`
	RunScripts  bool     `yaml:"run_scripts"`
	ResolveDeps bool     `yaml:"resolve_deps"`
}

// readConfig reads and parses apkg.yaml
func readConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// fetchAPKIndex downloads and parses the APKINDEX.tar.gz from a given Alpine repo URL
type APKPackage struct {
	Name     string
	Version  string
	Filename string
	Deps     []string
}

// fetchAndParseAPKIndex downloads and parses the APKINDEX.tar.gz from a given Alpine repo URL
// fetchAndParseAPKIndex fetches APKINDEX from the exact repo URL provided
func fetchAndParseAPKIndex(repoURL string) (map[string]APKPackage, error) {
	repoURL = strings.TrimRight(repoURL, "/")
	indexURL := repoURL + "/APKINDEX.tar.gz"
	resp, err := http.Get(indexURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download APKINDEX: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch APKINDEX: status %d, content-type %s, body: %s", resp.StatusCode, resp.Header.Get("Content-Type"), string(body))
	}

	ct := resp.Header.Get("Content-Type")
	if !(strings.Contains(ct, "gzip") || strings.Contains(ct, "octet-stream")) {
		return nil, fmt.Errorf("unexpected content-type for APKINDEX: %s", ct)
	}

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tarReader := tar.NewReader(gzr)
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar read error: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && hdr.Name == "APKINDEX" {
			return parseAPKIndex(tarReader)
		}
	}
	return nil, fmt.Errorf("APKINDEX not found in archive")
}

// parseAPKIndex parses the APKINDEX file and returns a map of package name to APKPackage
func parseAPKIndex(r io.Reader) (map[string]APKPackage, error) {
	// Read the entire APKINDEX into memory
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	content := string(data)

	entries := strings.Split(content, "\n\n")
	pkgs := make(map[string]APKPackage)
	for _, entry := range entries {
		var name, version, depsLine string
		for _, line := range strings.Split(entry, "\n") {
			if len(line) < 2 || line[1] != ':' {
				continue
			}
			val := line[2:]
			switch line[0] {
			case 'P':
				name = val
			case 'V':
				version = val
			case 'D':
				depsLine = val
			}
		}
		if name != "" && version != "" {
			filename := name + "-" + version + ".apk"
			var deps []string
			if depsLine != "" {
				for _, dep := range strings.Fields(depsLine) {
					// Remove version constraints (e.g., 'libc.musl-x86_64.so.1 so:libc.musl-x86_64.so.1')
					deps = append(deps, strings.Split(dep, ">=")[0])
				}
			}
			pkgs[name] = APKPackage{Name: name, Version: version, Filename: filename, Deps: deps}
		}
	}
	return pkgs, nil
}

// InstalledPkg represents a record of an installed package and its version
// Used for tracking and upgrade logic
type InstalledPkg struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// readInstalledPkgs reads the installed packages file (installed.yaml)
func readInstalledPkgs(path string) (map[string]string, error) {
	pkgs := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return pkgs, nil // treat as empty
		}
		return nil, err
	}
	defer f.Close()
	var list []InstalledPkg
	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&list); err != nil {
		return nil, err
	}
	for _, p := range list {
		pkgs[p.Name] = p.Version
	}
	return pkgs, nil
}

// writeInstalledPkgs writes the installed packages file (installed.yaml)
func writeInstalledPkgs(path string, pkgs map[string]string) error {
	list := make([]InstalledPkg, 0, len(pkgs))
	for name, ver := range pkgs {
		list = append(list, InstalledPkg{Name: name, Version: ver})
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := yaml.NewEncoder(f)
	return enc.Encode(list)
}

// globalConfig is used for script handling
var globalConfig *Config

func main() {
	var err error
	// CLI flags
	configPath := flag.String("config", "apkg.yaml", "Path to config file")
	dryRun := flag.Bool("dry-run", false, "Show what would be done, but don't modify anything")
	verbose := flag.Bool("v", false, "Enable verbose output")
	flag.Parse()

	args := flag.Args()
	if len(args) > 0 && (args[0] == "add" || args[0] == "remove" || args[0] == "reinstall" || args[0] == "regen-indexes" || args[0] == "list-installed" || args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
			fmt.Println(`apkg - worse Alpine package manager

Usage:
  apkg [flags]                # Install/upgrade/uninstall to match config
  apkg add <pkg>              # Add a package to the config and install it
  apkg remove|del <pkg>       # Remove a package from the config and uninstall it
  apkg reinstall <pkg>        # Force reinstall a package
  apkg regen-indexes          # Regenerate installed file indexes
  apkg list-installed         # List installed packages and versions

Flags:
  -config <file>   Path to config file (default: apkg.yaml)
  -dry-run         Show what would be done, but don't modify anything
  -v               Enable verbose output
  -h, --help       Show this help message
`)
			os.Exit(0)
		}
		if args[0] == "list-installed" {
			installedPkgs, _ := readInstalledPkgs("installed.yaml")
			if len(installedPkgs) == 0 {
				fmt.Println("No packages installed.")
			} else {
				fmt.Println("Installed packages:")
				for name, ver := range installedPkgs {
					fmt.Printf("  %s %s\n", name, ver)
				}
			}
			os.Exit(0)
		}
		var cfg *Config
		cfg, err = readConfig(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[FATAL] Failed to read config: %v\n", err)
			os.Exit(1)
		}
		if args[0] == "regen-indexes" {
			installedPkgs, _ := readInstalledPkgs("installed.yaml")
			cfgPkgs := make(map[string]bool)
			for _, p := range cfg.Packages {
				cfgPkgs[p] = true
			}
			updatedPkgs := make(map[string]string)
			for pkg, ver := range installedPkgs {
				if !cfgPkgs[pkg] {
					fmt.Printf("Removing %s from installed.yaml (not in config)\n", pkg)
					continue
				}
				fmt.Printf("Regenerating file index for %s (%s)...\n", pkg, ver)
				apkFile := "staged/" + pkg + "-" + ver + ".apk"
				// Find repo for this package
				_, sourceRepo, err := fetchAndParseAllAPKIndexes(cfg.Repos)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[WARN] Could not fetch APKINDEX for regen: %v\n", err)
					continue
				}
				repo, ok := sourceRepo[pkg]
				if !ok {
					fmt.Fprintf(os.Stderr, "[WARN] Could not find repo for %s\n", pkg)
					continue
				}
				apkURL := strings.TrimRight(repo, "/") + "/" + pkg + "-" + ver + ".apk"
				fmt.Printf("[DEBUG] Downloading from: %s\n", apkURL)
				err = downloadFile(apkURL, apkFile)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[WARN] Failed to download %s: %v\n", pkg, err)
					continue
				}
				tmpDir := "regen-staging-" + pkg
				os.RemoveAll(tmpDir)
				if err = extractApk(apkFile, tmpDir); err != nil {
					fmt.Fprintf(os.Stderr, "[WARN] Failed to extract %s: %v\n", pkg, err)
					os.Remove(apkFile)
					continue
				}
				var files []string
				_ = filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return nil
					}
					rel, err := filepath.Rel(tmpDir, path)
					if err != nil || rel == "." {
						return nil
					}
					files = append(files, rel)
					return nil
				})
				if err = writeInstalledFiles(pkg, files); err != nil {
					fmt.Fprintf(os.Stderr, "[WARN] Failed to write index for %s: %v\n", pkg, err)
				}
				os.RemoveAll(tmpDir)
				os.Remove(apkFile)
				fmt.Printf("Regenerated index for %s (%d files)\n", pkg, len(files))
				updatedPkgs[pkg] = ver
			}
			if err = writeInstalledPkgs("installed.yaml", updatedPkgs); err != nil {
				fmt.Fprintf(os.Stderr, "[WARN] Failed to update installed.yaml: %v\n", err)
			}
			os.Exit(0)
		}
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: %s [flags] add|remove|reinstall <package>\n", os.Args[0])
			os.Exit(1)
		}
		var err error
		cfg, err = readConfig(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[FATAL] Failed to read config: %v\n", err)
			os.Exit(1)
		}
		pkg := args[1]
		changed := false
		if args[0] == "add" {
			for _, p := range cfg.Packages {
				if p == pkg {
					fmt.Printf("%s is already in the package list.\n", pkg)
					os.Exit(0)
				}
			}
			cfg.Packages = append(cfg.Packages, pkg)
			changed = true
			fmt.Printf("Added %s to package list.\n", pkg)
		} else if args[0] == "remove" {
			newPkgs := []string{}
			found := false
			for _, p := range cfg.Packages {
				if p == pkg {
					found = true
					continue
				}
				newPkgs = append(newPkgs, p)
			}
			if found {
				cfg.Packages = newPkgs
				changed = true
				fmt.Printf("Removed %s from package list.\n", pkg)
			} else {
				fmt.Printf("%s was not in the package list.\n", pkg)
			}
		} else if args[0] == "reinstall" {
			// Remove from installed.yaml and installed_files, but keep in config
			fmt.Printf("Reinstalling %s...\n", pkg)
			// Remove installed files if present
			installedPkgs, _ := readInstalledPkgs("installed.yaml")
			if ver, ok := installedPkgs[pkg]; ok {
				// Find repo for this package
				_, sourceRepo, err := fetchAndParseAllAPKIndexes(cfg.Repos)
				repo := ""
				if err == nil {
					repo = sourceRepo[pkg]
				}
				if err := uninstallPackage(pkg, ver, repo, cfg.InstallDir); err != nil {
					fmt.Fprintf(os.Stderr, "[WARN] Failed to uninstall %s: %v\n", pkg, err)
				} else {
					fmt.Printf("Uninstalled %s (%s)\n", pkg, ver)
				}
			}
			// Ensure it's in the config
			found := false
			for _, p := range cfg.Packages {
				if p == pkg {
					found = true
					break
				}
			}
			if !found {
				cfg.Packages = append(cfg.Packages, pkg)
				changed = true
				fmt.Printf("Added %s to package list.\n", pkg)
			}
			changed = true // always reinstall
		}
		if changed {
			f, err := os.Create(*configPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[FATAL] Failed to write config: %v\n", err)
				os.Exit(1)
			}
			defer f.Close()
			enc := yaml.NewEncoder(f)
			if err := enc.Encode(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "[FATAL] Failed to encode config: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Config updated. Applying changes...")
			// Re-run main logic to apply install/uninstall, but drop subcommand args
			newArgs := []string{os.Args[0]}
			for _, a := range os.Args[1:] {
				if a == "add" || a == "remove" || a == "reinstall" || a == "regen-indexes" {
					break
				}
				newArgs = append(newArgs, a)
			}
			err = syscall.Exec(os.Args[0], newArgs, os.Environ())
			if err != nil {
				fmt.Fprintf(os.Stderr, "[FATAL] Failed to re-exec: %v\n", err)
				os.Exit(1)
			}
		}
		os.Exit(0)
	}

	cfg, err := readConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Failed to read config: %v\n", err)
		os.Exit(1)
	}
	globalConfig = cfg
	if *verbose {
		fmt.Println("Using repos:", cfg.Repos)
		fmt.Println("Packages to install:", cfg.Packages)
	}

	// 1. Fetch and parse APKINDEX from all repos
	fmt.Println("Fetching APKINDEX from all repos...")
	pkgMap, sourceRepo, err := fetchAndParseAllAPKIndexes(cfg.Repos)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Error fetching APKINDEX: %v\n", err)
		os.Exit(2)
	}

	installedPkgsPath := "installed.yaml"
	installedPkgs, _ := readInstalledPkgs(installedPkgsPath)
	updatedPkgs := make(map[string]string)
	for k, v := range installedPkgs {
		updatedPkgs[k] = v
	}

	// Dependency resolution
	installSet := map[string]struct{}{}
	var resolveDeps bool = cfg.ResolveDeps
	var addWithDeps func(string)
	addWithDeps = func(pkg string) {
		if _, ok := installSet[pkg]; ok {
			return
		}
		installSet[pkg] = struct{}{}
		if resolveDeps {
			info, ok := pkgMap[pkg]
			if ok {
				for _, dep := range info.Deps {
					if dep != "" && dep != pkg {
						addWithDeps(dep)
					}
				}
			}
		}
	}
	for _, pkg := range cfg.Packages {
		addWithDeps(pkg)
	}
	toInstall := []string{}
	for pkg := range installSet {
		toInstall = append(toInstall, pkg)
	}
	for _, pkg := range toInstall {
		info, ok := pkgMap[pkg]
		if !ok {
			continue
		}
		curVer, already := installedPkgs[pkg]
		if already {
			if curVer == info.Version {
				fmt.Printf("%s (%s) is already installed. Skipping.\n", pkg, curVer)
				continue
			} else {
				fmt.Printf("%s: upgrading from %s to %s\n", pkg, curVer, info.Version)
			}
		} else {
			fmt.Printf("%s (%s) will be installed.\n", pkg, info.Version)
		}
		updatedPkgs[pkg] = info.Version
	}

	// Only download and extract packages that need install/upgrade
	if *dryRun {
		fmt.Println("[DRY-RUN] The following packages would be downloaded and installed:")
		for _, pkg := range toInstall {
			info := pkgMap[pkg]
			fmt.Printf("  %s (%s)\n", pkg, info.Version)
		}
		fmt.Println("[DRY-RUN] No changes made.")
		return
	}
	if err := os.MkdirAll("staged", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Failed to create staged dir: %v\n", err)
		os.Exit(3)
	}
	if err := os.MkdirAll("staging-2", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Failed to create staging-2 dir: %v\n", err)
		os.Exit(3)
	}
	for _, pkg := range toInstall {
		info, ok := pkgMap[pkg]
		if !ok {
			continue
		}
		repo, ok := sourceRepo[pkg]
		if !ok {
			fmt.Fprintf(os.Stderr, "[ERROR] No repo found for %s\n", pkg)
			continue
		}
		apkURL := strings.TrimRight(repo, "/") + "/" + info.Filename
		stagedPath := "staged/" + info.Filename
		fmt.Printf("Downloading %s (%s) from %s\n", info.Name, info.Version, apkURL)
		if err := downloadFile(apkURL, stagedPath); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Failed to download %s: %v\n", info.Name, err)
			continue
		}
		fmt.Printf("Staged: %s\n", stagedPath)

		// Extract .apk (tar.gz) into staging-2
		if err := extractApk(stagedPath, "staging-2/"+pkg); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Failed to extract %s: %v\n", info.Name, err)
			continue
		}
		fmt.Printf("Extracted %s to staging-2/%s\n", info.Filename, pkg)
	}

	if cfg.Install {
		if err := installPackages(toInstall, "staging-2", cfg.InstallDir); err != nil {
			fmt.Fprintf(os.Stderr, "[FATAL] Install failed: %v\n", err)
			os.Exit(4)
		} else {
			fmt.Printf("All packages installed to %s\n", cfg.InstallDir)
			if err := writeInstalledPkgs(installedPkgsPath, updatedPkgs); err != nil {
				fmt.Fprintf(os.Stderr, "[WARN] Failed to update installed.yaml: %v\n", err)
			}
			cleanupTempDirs()
		}
	} else {
		fmt.Println("Install step skipped (install: false in config)")
	}

	// Uninstall packages that are no longer in the config
	toUninstall := []string{}
	for pkg := range installedPkgs {
		found := false
		for _, want := range cfg.Packages {
			if pkg == want {
				found = true
				break
			}
		}
		if !found {
			toUninstall = append(toUninstall, pkg)
		}
	}
	for _, pkg := range toUninstall {
		ver := installedPkgs[pkg]
		repo := ""
		if sourceRepo != nil {
			repo = sourceRepo[pkg]
		}
		if err := uninstallPackage(pkg, ver, repo, cfg.InstallDir); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Failed to uninstall %s: %v\n", pkg, err)
		} else {
			fmt.Printf("Uninstalled %s (%s)\n", pkg, ver)
			delete(updatedPkgs, pkg)
			if err := writeInstalledPkgs(installedPkgsPath, updatedPkgs); err != nil {
				fmt.Fprintf(os.Stderr, "[WARN] Failed to update installed.yaml after uninstall: %v\n", err)
			}
		}
	}
}

// extractApk extracts a .apk (tar.gz) file to the given directory
func extractApk(apkPath, destDir string) error {
	f, err := os.Open(apkPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	skipNames := []string{
		".PKGINFO", ".post-install", ".post-upgrade", ".pre-deinstall", ".trigger",
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := hdr.Name
		// Skip unwanted files
		skip := false
		for _, s := range skipNames {
			if name == s || strings.HasPrefix(name, s+"/") {
				skip = true
				break
			}
		}
		if strings.HasPrefix(name, ".SIGN.RSA-") {
			skip = true
		}
		if strings.HasSuffix(name, ".pub") {
			skip = true
		}
		if skip {
			continue
		}
		target := filepath.Join(destDir, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}

// installPackages copies files from stagingDir/pkg to installDir for each package, preserving structure and permissions.
func installPackages(pkgs []string, stagingDir, installDir string) error {
	for _, pkg := range pkgs {
		pkgStagingPath := filepath.Join(stagingDir, pkg)
		var installedFiles []string
		err := filepath.Walk(pkgStagingPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			relPath, err := filepath.Rel(pkgStagingPath, path)
			if err != nil || relPath == "." {
				return nil
			}
			targetPath := filepath.Join(installDir, relPath)
			if info.IsDir() {
				return os.MkdirAll(targetPath, info.Mode())
			}
			srcFile, err := os.Open(path)
			if err != nil {
				return err
			}
			defer srcFile.Close()
			dstFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
			if err != nil {
				return err
			}
			defer dstFile.Close()
			_, err = io.Copy(dstFile, srcFile)
			if err == nil {
				installedFiles = append(installedFiles, relPath)
			}
			return err
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Failed to copy files for package %s: %v\n", pkg, err)
			return fmt.Errorf("failed to install package %s: %w", pkg, err)
		}
		if err := writeInstalledFiles(pkg, installedFiles); err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] Failed to record installed files for %s: %v\n", pkg, err)
		}
		fmt.Printf("Installed package: %s to %s\n", pkg, installDir)

		// Script handling: look for known scripts and run or log
		scriptNames := []string{".post-install", ".pre-deinstall", ".post-upgrade"}
		for _, script := range scriptNames {
			scriptPath := filepath.Join(pkgStagingPath, script)
			if _, err := os.Stat(scriptPath); err == nil {
				if globalConfig != nil && globalConfig.RunScripts {
					fmt.Printf("Would run script: %s\n", scriptPath)
					// Here you would actually run the script if not in test-root
				} else {
					fmt.Fprintf(os.Stderr, "[WARN] Script present but not run (run_scripts: false): %s\n", scriptPath)
				}
			} else if !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "[WARN] Error checking script %s: %v\n", scriptPath, err)
			}
		}
	}
	return nil
}

// writeInstalledFiles records the list of files installed for a package
func writeInstalledFiles(pkgName string, files []string) error {
	dir := "installed_files"
	os.MkdirAll(dir, 0755)
	f, err := os.Create(filepath.Join(dir, pkgName+".yaml"))
	if err != nil {
		return err
	}
	defer f.Close()
	enc := yaml.NewEncoder(f)
	return enc.Encode(files)
}

// readInstalledFiles reads the list of files installed for a package
func readInstalledFiles(pkgName string) ([]string, error) {
	f, err := os.Open(filepath.Join("installed_files", pkgName+".yaml"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var files []string
	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&files); err != nil {
		return nil, err
	}
	return files, nil
}

// downloadFile downloads a file from url and saves it to dest
func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

// cleanupTempDirs removes temporary directories after install
func cleanupTempDirs() {
	os.RemoveAll("staged")
	os.RemoveAll("staging-2")
}

// uninstallPackage removes files belonging to a package from installDir using the installed_files index
func uninstallPackage(pkgName, version, repo, installDir string) error {
	fmt.Printf("Uninstalling %s (%s)...\n", pkgName, version)
	files, err := readInstalledFiles(pkgName)
	if err != nil {
		return fmt.Errorf("could not read installed files index: %w", err)
	}
	// Remove files
	for _, rel := range files {
		target := filepath.Join(installDir, rel)
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "[WARN] Failed to remove %s: %v\n", target, err)
		}
	}
	// Collect all parent directories
	dirs := map[string]struct{}{}
	for _, rel := range files {
		dir := filepath.Dir(filepath.Join(installDir, rel))
		if dir != installDir {
			dirs[dir] = struct{}{}
		}
	}
	// Get all files from other installed packages
	otherFiles := map[string]struct{}{}
	installedPkgs, _ := readInstalledPkgs("installed.yaml")
	for otherPkg := range installedPkgs {
		if otherPkg == pkgName {
			continue
		}
		ofs, _ := readInstalledFiles(otherPkg)
		for _, f := range ofs {
			otherFiles[filepath.Join(installDir, f)] = struct{}{}
		}
	}
	// Remove directories if empty and not used by other packages
	// Sort dirs by descending length (deepest first)
	dirList := []string{}
	for d := range dirs {
		dirList = append(dirList, d)
	}
	sort.Slice(dirList, func(i, j int) bool { return len(dirList[i]) > len(dirList[j]) })
	for _, dir := range dirList {
		used := false
		for f := range otherFiles {
			if strings.HasPrefix(f, dir+string(os.PathSeparator)) {
				used = true
				break
			}
		}
		if !used {
			// Only remove if empty
			_ = os.Remove(dir)
		}
	}
	os.Remove(filepath.Join("installed_files", pkgName+".yaml"))
	return nil
}

// fetchAndParseAllAPKIndexes fetches and merges APKINDEX from all repos
func fetchAndParseAllAPKIndexes(repos []string) (map[string]APKPackage, map[string]string, error) {
	pkgMap := make(map[string]APKPackage)
	sourceRepo := make(map[string]string) // package name -> repo URL
	for _, repo := range repos {
		m, err := fetchAndParseAPKIndex(repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] Failed to fetch APKINDEX from %s: %v\n", repo, err)
			continue
		}
		for name, pkg := range m {
			if _, exists := pkgMap[name]; !exists {
				pkgMap[name] = pkg
				sourceRepo[name] = repo
			}
		}
	}
	if len(pkgMap) == 0 {
		return nil, nil, fmt.Errorf("no packages found in any repo")
	}
	return pkgMap, sourceRepo, nil
}
