package updater

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/mod/modfile"
)

func TestNormalizeRequireBlocksSeparatesAndSortsDependencies(t *testing.T) {
	root := t.TempDir()
	goMod := `module fixture.test/root

go 1.26.7
toolchain go1.26.7

require (
	example.test/z v1.0.0
	example.test/indirect-z v1.2.0 // indirect
	example.test/a v1.0.0 // direct note
)

require example.test/indirect-a v1.0.0 // indirect

exclude example.test/excluded v1.2.3
`
	writeModule(t, root, goMod)
	if err := NormalizeRequireBlocks(root); err != nil {
		t.Fatalf("NormalizeRequireBlocks() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatalf("parse normalized go.mod: %v", err)
	}
	direct, indirect := requireBlockPaths(t, file)
	if !slices.Equal(direct, []string{"example.test/a", "example.test/z"}) {
		t.Fatalf("direct require paths = %v", direct)
	}
	if !slices.Equal(indirect, []string{"example.test/indirect-a", "example.test/indirect-z"}) {
		t.Fatalf("indirect require paths = %v", indirect)
	}
	if file.Go == nil || file.Go.Version != testBaselineGo {
		t.Fatalf("Go directive changed: got %+v, want version 1.26.7", file.Go)
	}
	if file.Toolchain == nil || file.Toolchain.Name != "go1.26.7" {
		t.Fatalf("toolchain directive changed: got %+v, want name go1.26.7", file.Toolchain)
	}
	if len(file.Exclude) != 1 {
		t.Fatalf("exclude directives changed: got %v, want one exclusion", file.Exclude)
	}
}

func TestNormalizeRequireBlocksKeepsExactlyTwoBlocksWhenAClassIsEmpty(t *testing.T) {
	tests := []struct {
		name         string
		goMod        string
		wantDirect   []string
		wantIndirect []string
	}{
		{
			name:       "no indirect dependencies",
			goMod:      "module fixture.test/root\n\ngo 1.26.7\n\nrequire example.test/direct v1.0.0\n",
			wantDirect: []string{"example.test/direct"},
		},
		{
			name:  "no dependencies",
			goMod: "module fixture.test/root\n\ngo 1.26.7\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeModule(t, root, tt.goMod)
			if err := NormalizeRequireBlocks(root); err != nil {
				t.Fatalf("NormalizeRequireBlocks() error = %v", err)
			}
			data, err := os.ReadFile(filepath.Join(root, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			file, err := modfile.Parse("go.mod", data, nil)
			if err != nil {
				t.Fatalf("parse normalized go.mod: %v", err)
			}
			direct, indirect := requireBlockPaths(t, file)
			if !slices.Equal(direct, tt.wantDirect) || !slices.Equal(indirect, tt.wantIndirect) {
				t.Fatalf("require groups = direct %v, indirect %v", direct, indirect)
			}
		})
	}
}

func requireBlockPaths(t *testing.T, file *modfile.File) ([]string, []string) {
	t.Helper()
	var blocks []*modfile.LineBlock
	for _, statement := range file.Syntax.Stmt {
		switch statement := statement.(type) {
		case *modfile.Line:
			if len(statement.Token) > 0 && statement.Token[0] == requireDirective {
				t.Fatal("found standalone require line; want require blocks only")
			}
		case *modfile.LineBlock:
			if len(statement.Token) > 0 && statement.Token[0] == requireDirective {
				blocks = append(blocks, statement)
			}
		}
	}
	if len(blocks) != 2 {
		t.Fatalf("found %d require blocks, want exactly two", len(blocks))
	}
	direct := make([]string, 0, len(blocks[0].Line))
	indirect := make([]string, 0, len(blocks[1].Line))
	for _, line := range blocks[0].Line {
		direct = append(direct, line.Token[0])
	}
	for _, line := range blocks[1].Line {
		indirect = append(indirect, line.Token[0])
	}
	return direct, indirect
}
