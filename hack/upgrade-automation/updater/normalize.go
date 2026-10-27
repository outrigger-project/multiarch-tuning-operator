package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/mod/modfile"
)

func NormalizeRequireBlocks(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve go.mod path: %w", err)
	}
	path := filepath.Join(root, "go.mod")
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat go.mod: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read go.mod: %w", err)
	}
	file, err := modfile.Parse(path, data, nil)
	if err != nil {
		return fmt.Errorf("parse go.mod: %w", err)
	}
	file.SetRequireAtMostTwo(file.Require)
	file.Cleanup()

	indirectByPath := make(map[string]bool, len(file.Require))
	for _, requirement := range file.Require {
		indirectByPath[requirement.Mod.Path] = requirement.Indirect
	}
	var directLines, indirectLines []*modfile.Line
	var directComments, indirectComments modfile.Comments
	statements := make([]modfile.Expr, 0, len(file.Syntax.Stmt)+2)
	insertAt := -1
	for _, statement := range file.Syntax.Stmt {
		switch node := statement.(type) {
		case *modfile.Line:
			if len(node.Token) != 0 && node.Token[0] == "require" {
				if insertAt < 0 {
					insertAt = len(statements)
				}
				if len(node.Token) < 3 {
					return fmt.Errorf("malformed require directive in go.mod")
				}
				modulePath := node.Token[1]
				line := cloneModLine(node)
				line.Token = append([]string(nil), node.Token[1:]...)
				line.InBlock = true
				if indirectByPath[modulePath] {
					indirectLines = append(indirectLines, line)
					appendModfileComments(&indirectComments, modfile.Comments{After: line.Comments.After})
					line.Comments.After = nil
				} else {
					directLines = append(directLines, line)
					appendModfileComments(&directComments, modfile.Comments{After: line.Comments.After})
					line.Comments.After = nil
				}
				continue
			}
		case *modfile.LineBlock:
			if len(node.Token) != 0 && node.Token[0] == "require" {
				if insertAt < 0 {
					insertAt = len(statements)
				}
				hasDirect, hasIndirect := false, false
				for _, existing := range node.Line {
					if len(existing.Token) < 2 {
						return fmt.Errorf("malformed require entry in go.mod")
					}
					modulePath := existing.Token[0]
					line := cloneModLine(existing)
					line.InBlock = true
					if indirectByPath[modulePath] {
						hasIndirect = true
						indirectLines = append(indirectLines, line)
					} else {
						hasDirect = true
						directLines = append(directLines, line)
					}
				}
				if hasIndirect && !hasDirect {
					appendModfileComments(&indirectComments, node.Comments)
				} else {
					appendModfileComments(&directComments, node.Comments)
				}
				continue
			}
		}
		statements = append(statements, statement)
	}

	sort.Slice(directLines, func(i, j int) bool { return directLines[i].Token[0] < directLines[j].Token[0] })
	sort.Slice(indirectLines, func(i, j int) bool { return indirectLines[i].Token[0] < indirectLines[j].Token[0] })
	directBlock := &modfile.LineBlock{Comments: directComments, Token: []string{"require"}, Line: directLines}
	indirectBlock := &modfile.LineBlock{Comments: indirectComments, Token: []string{"require"}, Line: indirectLines}
	if insertAt < 0 {
		insertAt = len(statements)
		for index, statement := range statements {
			if beforeRequireBlocks(statement) {
				insertAt = index
				break
			}
		}
	}
	if insertAt > len(statements) {
		insertAt = len(statements)
	}
	statements = append(statements, nil, nil)
	copy(statements[insertAt+2:], statements[insertAt:len(statements)-2])
	statements[insertAt] = directBlock
	statements[insertAt+1] = indirectBlock
	file.Syntax.Stmt = statements

	formatted := modfile.Format(file.Syntax)
	if err := verifyRequireBlocks(path, formatted, file.Require); err != nil {
		return err
	}
	if err := writeFileAtomically(path, formatted, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write normalized go.mod: %w", err)
	}
	return nil
}

func cloneModLine(line *modfile.Line) *modfile.Line {
	copy := *line
	copy.Token = append([]string(nil), line.Token...)
	copy.Comments.Before = append([]modfile.Comment(nil), line.Comments.Before...)
	copy.Comments.Suffix = append([]modfile.Comment(nil), line.Comments.Suffix...)
	copy.Comments.After = append([]modfile.Comment(nil), line.Comments.After...)
	return &copy
}

func appendModfileComments(destination *modfile.Comments, source modfile.Comments) {
	destination.Before = append(destination.Before, source.Before...)
	destination.Suffix = append(destination.Suffix, source.Suffix...)
	destination.After = append(destination.After, source.After...)
}

func beforeRequireBlocks(statement modfile.Expr) bool {
	var tokens []string
	switch node := statement.(type) {
	case *modfile.Line:
		tokens = node.Token
	case *modfile.LineBlock:
		tokens = node.Token
	default:
		return false
	}
	if len(tokens) == 0 {
		return false
	}
	switch tokens[0] {
	case "exclude", "replace", "retract", "godebug", "tool", "ignore":
		return true
	default:
		return false
	}
}

func verifyRequireBlocks(path string, data []byte, expected []*modfile.Require) error {
	parsed, err := modfile.Parse(path, data, nil)
	if err != nil {
		return fmt.Errorf("parse normalized go.mod: %w", err)
	}
	var blocks []*modfile.LineBlock
	for _, statement := range parsed.Syntax.Stmt {
		switch node := statement.(type) {
		case *modfile.Line:
			if len(node.Token) != 0 && node.Token[0] == "require" {
				return fmt.Errorf("normalized go.mod contains a standalone require directive")
			}
		case *modfile.LineBlock:
			if len(node.Token) != 0 && node.Token[0] == "require" {
				blocks = append(blocks, node)
			}
		}
	}
	if len(blocks) != 2 {
		return fmt.Errorf("normalized go.mod has %d require blocks, want exactly two", len(blocks))
	}
	indirectByPath := make(map[string]bool, len(parsed.Require))
	for _, requirement := range parsed.Require {
		indirectByPath[requirement.Mod.Path] = requirement.Indirect
	}
	for index, block := range blocks {
		for _, line := range block.Line {
			if len(line.Token) < 2 {
				return fmt.Errorf("normalized require block contains malformed entry")
			}
			indirect, exists := indirectByPath[line.Token[0]]
			if !exists {
				return fmt.Errorf("normalized require block contains unknown module %q", line.Token[0])
			}
			if index == 0 && indirect {
				return fmt.Errorf("direct require block contains indirect module %q", line.Token[0])
			}
			if index == 1 && !indirect {
				return fmt.Errorf("indirect require block contains direct module %q", line.Token[0])
			}
		}
	}
	if len(parsed.Require) != len(expected) {
		return fmt.Errorf("normalized go.mod changed the require count from %d to %d", len(expected), len(parsed.Require))
	}
	expectedByPath := make(map[string]modfile.Require, len(expected))
	for _, requirement := range expected {
		expectedByPath[requirement.Mod.Path] = *requirement
	}
	for _, requirement := range parsed.Require {
		want, exists := expectedByPath[requirement.Mod.Path]
		if !exists || want.Mod.Version != requirement.Mod.Version || want.Indirect != requirement.Indirect {
			return fmt.Errorf("normalized go.mod changed requirement %s@%s", requirement.Mod.Path, requirement.Mod.Version)
		}
		delete(expectedByPath, requirement.Mod.Path)
	}
	if len(expectedByPath) != 0 {
		return fmt.Errorf("normalized go.mod removed requirements: %v", expectedByPath)
	}
	return nil
}
