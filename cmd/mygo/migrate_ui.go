package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/internal/uimigrate"
)

func runMigrateUI(args []string) error {
	f := flag.NewFlagSet("migrate-ui", flag.ContinueOnError)
	write := f.Bool("write", false, "write changes (by default only list affected files)")
	if err := f.Parse(args); err != nil {
		return err
	}
	dir := "."
	if f.NArg() > 1 {
		return fmt.Errorf("migrate-ui accepts one directory")
	}
	if f.NArg() == 1 {
		dir = f.Arg(0)
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "build", ".mygo":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result, err := uimigrate.File(path, source)
		if err != nil {
			return err
		}
		for _, note := range result.Notes {
			fmt.Printf("%s: %s\n", path, note)
		}
		if !result.Changed {
			return nil
		}
		if *write {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, result.Source, info.Mode().Perm()); err != nil {
				return err
			}
		}
		fmt.Println(path)
		return nil
	})
}
