// Command seed is the Seed language toolchain: it compiles .seed source
// through AMIVM-IR (via the external amivm CLI) and go build into a
// native executable. The exact subcommand/flag surface is a placeholder
// until it's designed properly (see CLAUDE.md's development plan); for
// now it only supports `build` and `run`.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: seed <build|run> <file.seed>")
	}
	subcmd, srcPath := args[0], args[1]

	switch subcmd {
	case "build":
		outPath := defaultOutPath(srcPath)
		if err := compile(srcPath, outPath); err != nil {
			return err
		}
		fmt.Println(outPath)
		return nil

	case "run":
		tmp, err := os.MkdirTemp("", "seed-run-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)

		binPath := tmp + "/a.out"
		if err := compile(srcPath, binPath); err != nil {
			return err
		}

		runCmd := exec.Command(binPath)
		runCmd.Stdin = os.Stdin
		runCmd.Stdout = os.Stdout
		runCmd.Stderr = os.Stderr
		if err := runCmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			return err
		}
		return nil

	default:
		return fmt.Errorf("unknown subcommand %q (expected build or run)", subcmd)
	}
}

func defaultOutPath(srcPath string) string {
	return strings.TrimSuffix(srcPath, ".seed")
}
