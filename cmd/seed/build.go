package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"seed/internal/codegen"
	"seed/internal/parser"
	"seed/internal/sema"
)

// compile runs the full Seed → AMIVM-IR → Go → binary pipeline for a
// single .seed source file and writes the resulting executable to outPath.
//
// amivm's own build requires its output directory to be a Go module (its
// cross-package type-checking is module-aware), so the intermediate Go
// file is generated inside a scratch module rather than as a bare file.
func compile(srcPath, outPath string) error {
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}

	file, err := parser.Parse(string(src))
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	if err := sema.Check(file); err != nil {
		return fmt.Errorf("semantic error: %w", err)
	}

	ir, err := codegen.Generate(file)
	if err != nil {
		return fmt.Errorf("codegen error: %w", err)
	}

	workDir, err := os.MkdirTemp("", "seed-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	irPath := filepath.Join(workDir, "main.ir")
	if err := os.WriteFile(irPath, []byte(ir), 0o644); err != nil {
		return err
	}

	goPath := filepath.Join(workDir, "main.go")
	if out, err := exec.Command("amivm", irPath, "-o", goPath).CombinedOutput(); err != nil {
		return fmt.Errorf("amivm:\n%s", out)
	}

	modPath := filepath.Join(workDir, "go.mod")
	if err := os.WriteFile(modPath, []byte("module seedbuild\n\ngo 1.21\n"), 0o644); err != nil {
		return err
	}

	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}
	buildCmd := exec.Command("go", "build", "-o", absOut, ".")
	buildCmd.Dir = workDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build:\n%s", out)
	}

	return nil
}
