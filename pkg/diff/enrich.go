package diff

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

func EnrichCmpDiff(diff string) string {
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

func AltEnrichUnifiedDiff(diff string) string {
	if diff == "" {
		return ""
	}

	ud, err := ParseUnifiedDiff(diff)
	if err != nil {
		panic(err)
	}

	return ud.PrettyPrint()
}

func EnrichUnifiedDiff(diff string) string {
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

	// split the lines by \n and trim the common receding whitespace
	lines := strings.Split(diff, "\n")
	commonWhitespace := ""
	for _, line := range lines[0] {
		if line == ' ' || line == '\t' {
			commonWhitespace += string(line)
		} else {
			break
		}
	}
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, commonWhitespace)
	}
	diff = strings.Join(lines, "\n")

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
							found = "\t  "
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
