package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the structure of apkg.yaml
type Config struct {
	Repo     string   `yaml:"repo"`
	Packages []string `yaml:"packages"`
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
		var name, version string
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
				// case 'A':
				//     arch = val
			}
		}
		if name != "" && version != "" {
			// Construct filename as in apk-tools: ${name}-${version}.apk
			filename := name + "-" + version + ".apk"
			pkgs[name] = APKPackage{Name: name, Version: version, Filename: filename}
		}
	}
	return pkgs, nil
}

func main() {
	cfg, err := readConfig("apkg.yaml")
	if err != nil {
		fmt.Println("Failed to read config:", err)
		return
	}
	fmt.Println("Using repo:", cfg.Repo)
	fmt.Println("Packages to install:", cfg.Packages)

	// 1. Fetch and parse APKINDEX from parent dir
	fmt.Println("Fetching APKINDEX...")
	pkgMap, err := fetchAndParseAPKIndex(cfg.Repo)
	if err != nil {
		fmt.Println("Error fetching APKINDEX:", err)
		return
	}

	// 2. For each package, find in APKINDEX, download .apk from same dir, and stage
	os.MkdirAll("staged", 0755)
	os.MkdirAll("staging-2", 0755)
	for _, pkg := range cfg.Packages {
		info, ok := pkgMap[pkg]
		if !ok {
			fmt.Printf("Package not found in repo: %s\n", pkg)
			continue
		}
		apkURL := strings.TrimRight(cfg.Repo, "/") + "/" + info.Filename
		stagedPath := "staged/" + info.Filename
		fmt.Printf("Downloading %s (%s) from %s\n", info.Name, info.Version, apkURL)
		if err := downloadFile(apkURL, stagedPath); err != nil {
			fmt.Printf("Failed to download %s: %v\n", info.Name, err)
			continue
		}
		fmt.Printf("Staged: %s\n", stagedPath)

		// Extract .apk (tar.gz) into staging-2
		if err := extractApk(stagedPath, "staging-2"); err != nil {
			fmt.Printf("Failed to extract %s: %v\n", info.Name, err)
			continue
		}
		fmt.Printf("Extracted %s to staging-2/\n", info.Filename)
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
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, hdr.Name)
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
