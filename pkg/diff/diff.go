package diff

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/fatih/color"
	"github.com/google/go-cmp/cmp"
	"github.com/k0kubun/pp/v3"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/sergi/go-diff/diffmatchpatch"
)

func TypedDiffExportedOnly[T any](want T, got T) string {
	printer := pp.New()
	printer.SetExportedOnly(true)
	printer.SetColoringEnabled(false)

	return diffTyped(printer, want, got)
}

func TypedDiff[T any](want T, got T) string {
	printer := pp.New()
	printer.SetExportedOnly(false)
	printer.SetColoringEnabled(false)

	return diffTyped(printer, want, got)
}

func diffd(want string, got string) string {
	diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(want),
		B:        difflib.SplitLines(got),
		FromFile: "Expected",
		FromDate: "",
		ToFile:   "Actual",
		ToDate:   "",
		Context:  5,
	})

	return diff

}

// formatStartingWhitespace formats leading whitespace characters to be visible while maintaining proper spacing
// Example:
//
//	Input:  "    \t  hello"
//	Output: "····→···hello"
//
// Where:
//
//	· represents a space (Middle Dot U+00B7)
//	→ represents a tab (Rightwards Arrow U+2192)
func formatStartingWhitespace(s string, colord *color.Color) string {
	out := color.New(color.Bold).Sprint(" | ")
	for j, char := range s {
		switch char {
		case ' ':
			out += color.New(color.Faint, color.FgHiGreen).Sprint("∙") // ⌷
		case '\t':
			out += color.New(color.Faint, color.FgHiGreen).Sprint("→   ") // → └──▹
		default:
			return out + colord.Sprint(s[j:])
		}
	}
	return out
}

func enrichCmpDiff(diff string) string {
	if diff == "" {
		return ""
	}
	prevNoColor := color.NoColor
	defer func() {
		color.NoColor = prevNoColor
	}()
	color.NoColor = false

	expectedPrefix := fmt.Sprintf("[%s] %s", color.New(color.FgBlue, color.Bold).Sprint("want"), color.New(color.Faint).Sprint(" +"))
	actualPrefix := fmt.Sprintf("[%s] %s", color.New(color.Bold, color.FgRed).Sprint("got"), color.New(color.Faint).Sprint("  -"))

	str := "\n"

	// Process each line
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			str += line + "\n"
			continue
		}

		// Format the line based on its content
		switch {
		case strings.HasPrefix(line, "-"):
			content := strings.TrimPrefix(line, "-")
			str += actualPrefix + " | " + color.New(color.FgRed).Sprint(content) + "\n"
		case strings.HasPrefix(line, "+"):
			content := strings.TrimPrefix(line, "+")
			str += expectedPrefix + " | " + color.New(color.FgBlue).Sprint(content) + "\n"
		default:
			str += strings.Repeat(" ", 9) + " | " + color.New(color.Faint).Sprint(line) + "\n"
		}
	}

	return str
}

func diffTyped[T any](printer *pp.PrettyPrinter, want T, got T) string {
	// Enable colors

	// printer.WithLineInfo = true

	switch any(want).(type) {
	case reflect.Type:
		want := ConvolutedFormatReflectType(any(want).(reflect.Type))
		got := ConvolutedFormatReflectType(any(got).(reflect.Type))
		return diffTyped[string](printer, want, got)
	case reflect.Value:
		w := any(want).(reflect.Value)
		g := any(got).(reflect.Value)
		want := ConvolutedFormatReflectValue(w)
		got := ConvolutedFormatReflectValue(g)
		return diffTyped[any](printer, want, got)
	case string:
		unified := diffd(any(want).(string), any(got).(string))

		return enrichUnifiedDiff(unified)
	default:
		cmpd := cmp.Diff(got, want)
		return enrichCmpDiff(cmpd)
	}
}

func enrichUnifiedDiff(diff string) string {
	if diff == "" {
		return ""
	}
	prevNoColor := color.NoColor
	defer func() {
		color.NoColor = prevNoColor
	}()
	color.NoColor = false

	expectedPrefix := fmt.Sprintf("[%s] %s", color.New(color.FgBlue, color.Bold).Sprint("want"), color.New(color.Faint).Sprint(" +"))
	actualPrefix := fmt.Sprintf("[%s] %s", color.New(color.Bold, color.FgRed).Sprint("got"), color.New(color.Faint).Sprint("  -"))

	diff = strings.ReplaceAll(diff, "--- Expected", fmt.Sprintf("%s %s [%s]", color.New(color.Faint).Sprint("---"), color.New(color.FgBlue).Sprint("want"), color.New(color.FgBlue, color.Bold).Sprint("want")))
	diff = strings.ReplaceAll(diff, "+++ Actual", fmt.Sprintf("%s %s [%s]", color.New(color.Faint).Sprint("+++"), color.New(color.FgRed).Sprint("got"), color.New(color.FgRed, color.Bold).Sprint("got")))

	realignmain := []string{}
	for i, spltz := range strings.Split(diff, "\n@@") {
		if i == 0 {
			realignmain = append(realignmain, spltz)
		} else {
			first := ""

			realign := []string{}
			for j, found := range strings.Split(spltz, "\n") {
				if j == 0 {
					first = color.New(color.Faint).Sprint("@@" + found)
				} else {
					if strings.HasPrefix(found, "-") {
						realign = append(realign, expectedPrefix+formatStartingWhitespace(found[1:], color.New(color.FgBlue)))
					} else if strings.HasPrefix(found, "+") {
						realign = append(realign, actualPrefix+formatStartingWhitespace(found[1:], color.New(color.FgRed)))
					} else {
						if found == "" {
							found = "  "
						}
						realign = append(realign, strings.Repeat(" ", 9)+formatStartingWhitespace(found[1:], color.New(color.Faint)))
					}
				}
			}

			realignmain = append(realignmain, first)
			realignmain = append(realignmain, realign...)
		}
		realignmain = append(realignmain, "")
	}
	str := "\n"
	str += strings.Join(realignmain, "\n")
	return str
}

func SingleLineStringDiff(want string, got string) string {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(want, got, false)
	return dmp.DiffPrettyText(diffs)
}
