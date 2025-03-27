package diff

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/k0kubun/pp/v3"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/sourcegraph/go-diff/diff"
)

type UnifiedDiff struct {
	FileDiff *diff.FileDiff
}

// highlightChanges finds the specific changes between two lines and returns a formatted string
// with just the changes bolded while keeping the rest of the line with normal styling
func highlightChanges(oldLine, newLine string, lineColor *color.Color) string {
	dmp := diffmatchpatch.New()
	// Use character-level diffing for better precision
	diffs := dmp.DiffMain(oldLine, newLine, false)
	// Enable cleanup to merge nearby edits for more meaningful highlighting
	diffs = dmp.DiffCleanupSemantic(diffs)

	var result strings.Builder
	for _, d := range diffs {
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			// For insertions, show the added text in bold with the line color
			result.WriteString(color.New(color.FgBlue, color.Bold).Sprint(d.Text))
		case diffmatchpatch.DiffEqual:
			// For unchanged text, use normal styling with the original line color
			result.WriteString(lineColor.Add(color.Faint).Sprint(d.Text))
		case diffmatchpatch.DiffDelete:
			// Skip deletions when showing the new line
			// They are shown in the old line
		}
	}
	return result.String()
}

// highlightDeletedText shows the old line with deleted parts highlighted
func highlightDeletedText(oldLine, newLine string, lineColor *color.Color) string {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(oldLine, newLine, false)
	diffs = dmp.DiffCleanupSemantic(diffs)

	var result strings.Builder
	for _, d := range diffs {
		switch d.Type {
		case diffmatchpatch.DiffDelete:
			// For deletions, show the removed text in bold
			result.WriteString(color.New(color.FgRed, color.Bold).Sprint(d.Text))
		case diffmatchpatch.DiffEqual:
			// For unchanged text, use normal styling with the original line color
			result.WriteString(lineColor.Add(color.Faint).Sprint(d.Text))
		case diffmatchpatch.DiffInsert:
			// Skip insertions when showing the old line
			// They are shown in the new line
		}
	}
	return result.String()
}

func ParseUnifiedDiff(diffStr string) (*UnifiedDiff, error) {
	if diffStr == "" {
		return nil, errors.New("empty diff string")
	}

	lines := strings.Split(diffStr, "\n")
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
	diffStr = strings.Join(lines, "\n")

	fmt.Println(diffStr)

	// Parse the unified diff using sourcegraph/go-diff
	fileDiff, err := diff.ParseFileDiff([]byte(diffStr))
	if err != nil {
		// If parsing fails, fall back to our original implementation
		return nil, err
	}

	pp.Println(fileDiff)

	return &UnifiedDiff{
		FileDiff: fileDiff,
	}, nil
}

func (ud *UnifiedDiff) PrettyPrint() string {
	if ud == nil || ud.FileDiff == nil {
		return ""
	}

	prevNoColor := color.NoColor
	defer func() {
		color.NoColor = prevNoColor
	}()
	color.NoColor = false

	// Use more distinct colors for better readability
	expectedPrefix := fmt.Sprintf("[%s] %s", color.New(color.FgBlue, color.Bold).Sprint("want"), color.New(color.Faint).Sprint(" +"))
	actualPrefix := fmt.Sprintf("[%s] %s", color.New(color.Bold, color.FgRed).Sprint("got"), color.New(color.Faint).Sprint("  -"))

	var result []string

	// Add file headers with proper formatting
	if ud.FileDiff.OrigName != "" {
		result = append(result, fmt.Sprintf("%s %s [%s]",
			color.New(color.Faint).Sprint("---"),
			color.New(color.FgBlue).Sprint(ud.FileDiff.OrigName),
			color.New(color.FgBlue, color.Bold).Sprint("want")))
	}
	if ud.FileDiff.NewName != "" {
		result = append(result, fmt.Sprintf("%s %s [%s]",
			color.New(color.Faint).Sprint("+++"),
			color.New(color.FgRed).Sprint(ud.FileDiff.NewName),
			color.New(color.FgRed, color.Bold).Sprint("got")))
	}

	// Process each hunk
	for _, hunk := range ud.FileDiff.Hunks {
		// Add hunk header
		result = append(result, color.New(color.Faint).Sprintf("@@ -%d,%d +%d,%d @@%s",
			hunk.OrigStartLine, hunk.OrigLines,
			hunk.NewStartLine, hunk.NewLines,
			hunk.Section))

		// Process hunk body
		lines := strings.Split(string(hunk.Body), "\n")

		// Group related removals and additions for better comparison
		lineGroups := groupRelatedChanges(lines)

		// Process each group of related changes
		for _, group := range lineGroups {
			// Handle context lines
			for _, line := range group.contextLines {
				result = append(result, strings.Repeat(" ", 9)+formatStartingWhitespace(line, color.New(color.Faint)))
			}

			// Handle changes
			if len(group.oldLines) == 1 && len(group.newLines) == 1 {
				// One-to-one mapping - ideal for highlighting specific differences
				oldLine := group.oldLines[0]
				newLine := group.newLines[0]

				// Show original line with deleted parts highlighted
				redColor := color.New(color.FgRed)
				highlightedOld := highlightDeletedText(oldLine, newLine, redColor)
				result = append(result, actualPrefix+formatStartingWhitespace(highlightedOld, redColor))

				// Show new line with added parts highlighted
				blueColor := color.New(color.FgBlue)
				highlightedNew := highlightChanges(oldLine, newLine, blueColor)
				result = append(result, expectedPrefix+formatStartingWhitespace(highlightedNew, blueColor))
			} else {
				// Process all removals first
				for _, line := range group.oldLines {
					result = append(result, actualPrefix+formatStartingWhitespace(line, color.New(color.FgRed)))
				}

				// Then process all additions
				for _, line := range group.newLines {
					result = append(result, expectedPrefix+formatStartingWhitespace(line, color.New(color.FgBlue)))
				}
			}
		}

		result = append(result, "") // Add blank line between hunks
	}

	return "\n" + strings.Join(result, "\n")
}

// lineGroup represents a group of related changes in a diff
type lineGroup struct {
	contextLines []string
	oldLines     []string
	newLines     []string
}

// groupRelatedChanges processes diff lines and groups them into related changes
// This helps with matching corresponding add/remove lines for better diff highlighting
func groupRelatedChanges(lines []string) []lineGroup {
	var groups []lineGroup
	var currentGroup lineGroup

	// Helper to add the current group to groups and start a new one
	addGroup := func() {
		if len(currentGroup.contextLines) > 0 || len(currentGroup.oldLines) > 0 || len(currentGroup.newLines) > 0 {
			groups = append(groups, currentGroup)
			currentGroup = lineGroup{}
		}
	}

	inChange := false

	for _, line := range lines {
		if line == "" {
			continue
		}

		switch line[0] {
		case '-', '+':
			// We're processing a change
			inChange = true

			if line[0] == '-' {
				currentGroup.oldLines = append(currentGroup.oldLines, line[1:])
			} else {
				currentGroup.newLines = append(currentGroup.newLines, line[1:])
			}
		default:
			// Context line
			if inChange {
				// If we were processing changes and hit a context line,
				// finish the current group and start a new one
				addGroup()
				inChange = false
			}

			// Add to the current group's context lines
			if line == "" {
				line = "\t  "
			}
			currentGroup.contextLines = append(currentGroup.contextLines, line)
		}
	}

	// Add the final group if it's not empty
	addGroup()

	return groups
}
