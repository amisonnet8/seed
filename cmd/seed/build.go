package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"seed/internal/codegen"
	"seed/internal/parser"
	"seed/internal/sema"
	"seed/seedrt"
)

// compile runs the full Seed → AMIVM-IR → Go → binary pipeline for a
// single .seed source file and writes the resulting executable to outPath.
//
// amivm's own build requires its output directory to be a Go module (its
// cross-package type-checking is module-aware), so the intermediate Go
// file is generated inside a scratch module rather than as a bare file.
// seedrt's own source is copied into that same scratch module (see
// writeSeedrt) so the generated code's `import "seedrt"` always resolves
// locally, regardless of where the seed binary itself was built or is
// being run from.
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

	modPath := filepath.Join(workDir, "go.mod")
	if err := os.WriteFile(modPath, []byte("module seedbuild\n\ngo 1.21\n"), 0o644); err != nil {
		return err
	}
	if err := writeSeedrt(workDir); err != nil {
		return err
	}

	irPath := filepath.Join(workDir, "main.ir")
	if err := os.WriteFile(irPath, []byte(ir), 0o644); err != nil {
		return err
	}

	goPath := filepath.Join(workDir, "main.go")
	// -i is safe to pass unconditionally even for a program that never
	// calls into seedrt: amivm drops an unused import mapping on its own.
	amivmCmd := exec.Command("amivm", irPath, "-o", goPath, "-i", "seedrt=seedbuild/seedrt")
	if out, err := amivmCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("amivm:\n%s", out)
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

// writeSeedrt copies seedrt's own embedded source (see seedrt/embed.go)
// into workDir/seedrt, so it becomes an ordinary subpackage of the
// scratch build module — "seedbuild/seedrt" — with no separate module or
// replace directive needed. embed.go itself is skipped: copying it too
// would work (its own //go:embed *.go would just re-embed the copy,
// harmlessly), but the embed.FS it declares serves no purpose once
// copied out, so there's nothing to gain from including it.
func writeSeedrt(workDir string) error {
	dir := filepath.Join(workDir, "seedrt")
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(seedrt.Source, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || name == "embed.go" {
			return nil
		}
		content, err := fs.ReadFile(seedrt.Source, name)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, name), content, 0o644)
	})
}
