// Command build cross-compiles pruneplex for several platforms.
//
// Usage (from the repo root, on any OS with Go installed):
//
//	go run ./tools/build                  # all targets -> dist/
//	go run ./tools/build -target linux/arm64,windows/amd64
//	go run ./tools/build -version v1.2.3 -out release
//	go run ./tools/build -list
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var defaultTargets = []string{
	"linux/amd64",
	"linux/arm64",
	"darwin/amd64",
	"darwin/arm64",
	"windows/amd64",
	"windows/arm64",
}

func main() {
	var (
		targets = flag.String("target", "all", `comma-separated GOOS/GOARCH list, "all", or "host"`)
		out     = flag.String("out", "dist", "output directory")
		version = flag.String("version", "", "version string embedded in the file name (optional)")
		list    = flag.Bool("list", false, "list default targets and exit")
	)
	flag.Parse()

	if *list {
		fmt.Println(strings.Join(defaultTargets, "\n"))
		return
	}

	var selected []string
	switch *targets {
	case "all":
		selected = defaultTargets
	case "host":
		selected = []string{runtime.GOOS + "/" + runtime.GOARCH}
	default:
		selected = strings.Split(*targets, ",")
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}

	sums := map[string]string{}
	for _, t := range selected {
		t = strings.TrimSpace(t)
		goos, goarch, ok := strings.Cut(t, "/")
		if !ok || goos == "" || goarch == "" {
			fatal(fmt.Errorf("invalid target %q, want GOOS/GOARCH", t))
		}
		name := fmt.Sprintf("pruneplex-%s-%s", goos, goarch)
		if *version != "" {
			name = fmt.Sprintf("pruneplex-%s-%s-%s", *version, goos, goarch)
		}
		if goos == "windows" {
			name += ".exe"
		}
		path := filepath.Join(*out, name)

		fmt.Printf("building %-40s", name)
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", path, "./cmd/server")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Println("FAILED")
			fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fatal(err)
		}
		sums[name] = fmt.Sprintf("%x", sha256.Sum256(data))
		fmt.Println("ok")
	}

	names := make([]string, 0, len(sums))
	for n := range sums {
		names = append(names, n)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, n := range names {
		fmt.Fprintf(&sb, "%s  %s\n", sums[n], n)
	}
	if err := os.WriteFile(filepath.Join(*out, "SHA256SUMS"), []byte(sb.String()), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %d binaries and SHA256SUMS to %s/\n", len(names), *out)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
