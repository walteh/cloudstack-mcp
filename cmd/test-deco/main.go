// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// gotest is a tiny program that shells out to `go test`
// and prints the output in color.
package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/fatih/color"
	"github.com/google/go-cmp/cmp"
	"github.com/walteh/cloudstack-mcp/pkg/diff"
)

var (
	pass = color.FgGreen
	skip = color.FgYellow
	fail = color.FgHiRed

	skipnotest bool

	// Buffer for collecting diff lines
	diffBuffer []string
	inDiff     bool
)

const (
	paletteEnv     = "GOTEST_PALETTE"
	skipNoTestsEnv = "GOTEST_SKIPNOTESTS"
)

func main() {
	enablePalette()
	enableSkipNoTests()
	enableOnCI()

	os.Exit(gotest(os.Args[1:]))
}

func gotest(args []string) int {
	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Wait()

	r, w := io.Pipe()
	defer w.Close()

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stderr = w
	cmd.Stdout = w
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		log.Print(err)
		wg.Done()
		return 1
	}

	go consume(&wg, r)

	sigc := make(chan os.Signal, 1)
	done := make(chan struct{})
	defer func() {
		signal.Stop(sigc)
		close(sigc)
		done <- struct{}{}
	}()
	signal.Notify(sigc)

	go func() {
		for {
			select {
			case sig := <-sigc:
				cmd.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
			return ws.ExitStatus()
		}
		return 1
	}
	return 0
}

func consume(wg *sync.WaitGroup, r io.Reader) {
	defer wg.Done()
	reader := bufio.NewReader(r)
	var currentPackage string
	var currentTestFile string
	var currentTest string

	for {
		l, _, err := reader.ReadLine()
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Print(err)
			return
		}
		line := string(l)
		trimmed := strings.TrimSpace(line)

		// If we're in a diff block and hit a new test or the end of output,
		// process and print the buffered diff
		if inDiff && (strings.HasPrefix(trimmed, "=== RUN") || strings.HasPrefix(trimmed, "=== FAIL") || strings.HasPrefix(trimmed, "--- FAIL") || strings.HasPrefix(trimmed, "FAIL") || strings.HasPrefix(trimmed, "Test:")) {
			if len(diffBuffer) > 0 {
				processDiff(diffBuffer)
				diffBuffer = nil
				inDiff = false
			}
		}

		// Handle package run indicators
		if strings.HasPrefix(trimmed, "=== RUN") {
			parts := strings.Split(trimmed, " ")
			if len(parts) >= 3 {
				testName := parts[2]
				// Extract package name from test name (assuming TestName or PackageName/TestName format)
				var packageName string
				if idx := strings.LastIndex(testName, "/"); idx >= 0 {
					packageName = testName[:idx]
				} else {
					packageName = testName
				}

				if packageName != currentPackage && packageName != "" {
					if currentPackage != "" {
						fmt.Println(color.New(color.Faint).Sprint("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"))
					}
					currentPackage = packageName
					fmt.Printf("\n%s\n", color.New(color.Bold, color.FgHiBlue).Sprint("📦 Package: "+currentPackage))
				}
			}
			// Still print the RUN line in a more subtle way
			fmt.Printf("%s %s\n",
				color.New(color.Faint).Sprint("  "),
				color.New(color.Faint).Sprint(trimmed))
			continue
		}

		// Handle test failures
		if strings.HasPrefix(trimmed, "=== FAIL:") {
			parts := strings.Split(trimmed, " ")
			if len(parts) >= 3 {
				testName := parts[2]
				duration := ""
				if len(parts) >= 4 {
					duration = parts[3]
				}
				if testName != currentTest {
					if currentTest != "" {
						fmt.Println(color.New(color.Faint).Sprint("────────────────────────────────────────"))
					}
					currentTest = testName
					fmt.Printf("\n%s %s %s\n",
						color.New(fail, color.Bold).Sprint("✗"),
						color.New(color.Bold).Sprint(testName),
						color.New(color.Faint).Sprint(duration),
					)
				}
			} else {
				color.Set(fail)
				fmt.Printf("%s\n", line)
			}
			continue
		}

		// Handle test passes
		if strings.HasPrefix(trimmed, "--- PASS:") {
			parts := strings.Split(trimmed, " ")
			if len(parts) >= 3 {
				testName := parts[2]
				duration := ""
				if len(parts) >= 4 {
					duration = parts[3]
				}
				fmt.Printf("%s %s %s\n",
					color.New(pass, color.Bold).Sprint("✓"),
					color.New(color.Bold).Sprint(testName),
					color.New(color.Faint).Sprint(duration),
				)
			} else {
				color.Set(pass)
				fmt.Printf("%s\n", line)
			}
			continue
		}

		// Handle test errors
		if strings.HasPrefix(trimmed, "=== Failed") {
			fmt.Printf("\n%s %s\n",
				color.New(color.Bold, color.FgHiRed).Sprint("❌ Failed Tests:"),
				color.New(color.Bold).Sprint(strings.TrimPrefix(line, "=== Failed")),
			)
			continue
		}

		// For test failures, collect and process the diff
		if inDiff {
			diffBuffer = append(diffBuffer, line)
			continue
		}

		// For error messages in test failures
		if strings.Contains(trimmed, "Error:") {
			if strings.Contains(trimmed, "Not equal:") {
				inDiff = true
				diffBuffer = make([]string, 0)
				fmt.Printf("%s %s\n",
					color.New(color.Faint).Sprint("  "),
					color.New(color.Bold, color.FgHiRed).Sprint("⚠️  Not equal:"),
				)
				continue
			}
			fmt.Printf("%s %s\n",
				color.New(color.Faint).Sprint("  "),
				color.New(color.FgHiRed).Sprint(strings.TrimSpace(strings.TrimPrefix(line, "Error:"))),
			)
			continue
		}

		// For error traces in test failures
		if strings.Contains(trimmed, "Error Trace:") {
			fileInfo := strings.TrimSpace(strings.TrimPrefix(line, "Error Trace:"))
			if fileInfo != currentTestFile {
				currentTestFile = fileInfo
				fmt.Printf("\n%s %s\n",
					color.New(color.Faint).Sprint("  "),
					color.New(color.FgHiBlue).Sprint("📄 File: "+fileInfo),
				)
			}
			continue
		}

		// For test names in the output
		if strings.HasPrefix(trimmed, "Test:") {
			testName := strings.TrimSpace(strings.TrimPrefix(line, "Test:"))
			if testName != currentTest {
				fmt.Printf("%s %s\n",
					color.New(color.Faint).Sprint("  "),
					color.New(color.Bold).Sprint("🧪 "+testName),
				)
			}
			continue
		}

		// Format PASS/FAIL/SKIP summary lines
		if strings.HasPrefix(trimmed, "PASS") ||
			strings.HasPrefix(trimmed, "FAIL") ||
			strings.HasPrefix(trimmed, "SKIP") {

			if strings.HasPrefix(trimmed, "PASS") {
				fmt.Printf("\n%s %s\n",
					color.New(color.Bold, color.FgGreen).Sprint("✅ PASS"),
					color.New(color.Faint).Sprint(strings.TrimPrefix(trimmed, "PASS")))
			} else if strings.HasPrefix(trimmed, "FAIL") {
				fmt.Printf("\n%s %s\n",
					color.New(color.Bold, color.FgHiRed).Sprint("❌ FAIL"),
					color.New(color.Faint).Sprint(strings.TrimPrefix(trimmed, "FAIL")))
			} else if strings.HasPrefix(trimmed, "SKIP") {
				fmt.Printf("\n%s %s\n",
					color.New(color.Bold, color.FgYellow).Sprint("⏭️  SKIP"),
					color.New(color.Faint).Sprint(strings.TrimPrefix(trimmed, "SKIP")))
			}
			continue
		}

		// For coverage information

		// Pass through all other output as-is
		fmt.Println(line)
	}
}

func processDiff(lines []string) {
	// Extract the actual diff content
	var want, got string
	var currentSection string
	var diffStarted bool
	var unifiedDiff string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines
		if trimmed == "" {
			continue
		}

		// Look for the diff section
		if strings.Contains(trimmed, "Diff:") {
			diffStarted = true
			continue
		}

		if !diffStarted {
			// Detect which section we're in for the struct comparison
			switch {
			case strings.HasPrefix(trimmed, "expected:"):
				currentSection = "want"
				want = strings.TrimPrefix(trimmed, "expected:")
			case strings.HasPrefix(trimmed, "actual:"):
				currentSection = "got"
				got = strings.TrimPrefix(trimmed, "actual:")
			case strings.HasPrefix(trimmed, "actual  :"):
				currentSection = "got"
				got = strings.TrimPrefix(trimmed, "actual  :")
			default:
				if currentSection == "want" {
					want = trimmed
				} else if currentSection == "got" {
					got = trimmed
				}
			}
		} else {
			// Collect unified diff lines
			unifiedDiff += line + "\n"
		}
	}

	if diffStarted && unifiedDiff != "" {
		// Use the diff package to format the unified diff
		fmt.Print(diff.EnrichUnifiedDiff(unifiedDiff))
	} else if want != "" && got != "" {
		// If we have struct content but no diff, generate one
		formattedDiff := cmp.Diff(want, got)
		fmt.Print(diff.EnrichCmpDiff(formattedDiff))
	}
}

func formatDiffLine(line string) string {
	trimmed := strings.TrimSpace(line)
	indent := strings.Repeat(" ", len(line)-len(strings.TrimLeft(line, " ")))

	if strings.HasPrefix(trimmed, "+") {
		return fmt.Sprintf("%s%s", indent, color.New(color.FgGreen).Sprint(trimmed))
	}
	if strings.HasPrefix(trimmed, "-") {
		return fmt.Sprintf("%s%s", indent, color.New(color.FgRed).Sprint(trimmed))
	}
	return fmt.Sprintf("%s%s", indent, trimmed)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func enableOnCI() {
	ci := strings.ToLower(os.Getenv("CI"))
	switch ci {
	case "true":
		fallthrough
	case "travis":
		fallthrough
	case "appveyor":
		fallthrough
	case "gitlab_ci":
		fallthrough
	case "circleci":
		color.NoColor = false
	}
}

func enablePalette() {
	v := os.Getenv(paletteEnv)
	if v == "" {
		return
	}
	vals := strings.Split(v, ",")
	if len(vals) != 2 {
		return
	}
	if c, ok := colors[vals[0]]; ok {
		fail = c
	}
	if c, ok := colors[vals[1]]; ok {
		pass = c
	}
}

func enableSkipNoTests() {
	v := os.Getenv(skipNoTestsEnv)
	if v == "" {
		return
	}
	v = strings.ToLower(v)
	skipnotest = v == "true"
}

var colors = map[string]color.Attribute{
	"black":     color.FgBlack,
	"hiblack":   color.FgHiBlack,
	"red":       color.FgRed,
	"hired":     color.FgHiRed,
	"green":     color.FgGreen,
	"higreen":   color.FgHiGreen,
	"yellow":    color.FgYellow,
	"hiyellow":  color.FgHiYellow,
	"blue":      color.FgBlue,
	"hiblue":    color.FgHiBlue,
	"magenta":   color.FgMagenta,
	"himagenta": color.FgHiMagenta,
	"cyan":      color.FgCyan,
	"hicyan":    color.FgHiCyan,
	"white":     color.FgWhite,
	"hiwhite":   color.FgHiWhite,
}
