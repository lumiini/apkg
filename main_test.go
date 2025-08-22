package main

import (
	"os"
	"testing"
)

func TestReadConfig(t *testing.T) {
	f, err := os.CreateTemp("", "apkg-test-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("repo: test\npackages:\n  - foo\ninstall: true\ninstall_dir: root\nrun_scripts: false\n")
	f.Close()
	cfg, err := readConfig(f.Name())
	if err != nil {
		t.Fatalf("readConfig failed: %v", err)
	}
	if cfg.Repo != "test" || len(cfg.Packages) != 1 || cfg.Packages[0] != "foo" || !cfg.Install || cfg.InstallDir != "root" || cfg.RunScripts != false {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestInstalledPkgsReadWrite(t *testing.T) {
	path := "installed-test.yaml"
	pkgs := map[string]string{"foo": "1.0", "bar": "2.0"}
	if err := writeInstalledPkgs(path, pkgs); err != nil {
		t.Fatalf("writeInstalledPkgs failed: %v", err)
	}
	defer os.Remove(path)
	read, err := readInstalledPkgs(path)
	if err != nil {
		t.Fatalf("readInstalledPkgs failed: %v", err)
	}
	if len(read) != 2 || read["foo"] != "1.0" || read["bar"] != "2.0" {
		t.Errorf("unexpected read: %+v", read)
	}
}
