package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/openshift/multiarch-tuning-operator/hack/upgrade-automation/updater"
)

const defaultReportPath = "_output/dependency-upgrade-report.json"

const usage = `Policy-driven Go dependency updater

Usage:
  upgrade.sh --mode pin-both
  upgrade.sh --mode go-only --go-target 1.26.8
  upgrade.sh --mode kubernetes-only --k8s-target k8s.io/api=v0.36.6
  upgrade.sh --mode both --go-target 1.26.8 --k8s-target k8s.io/api=v0.36.6

Options:
  --mode         pin-both | go-only | kubernetes-only | both
  --go-target    exact Go patch version, required by go-only and both
  --k8s-target   repeatable exact module=version Kubernetes target
  --go-bin       Go compiler executable (default: go)
  --report       JSON report path (default: _output/dependency-upgrade-report.json)
`

type cliOptions struct {
	policy     updater.Policy
	goBinary   string
	reportPath string
}

type repeatedFlag []string

func (f *repeatedFlag) String() string { return strings.Join(*f, ",") }
func (f *repeatedFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func main() {
	os.Exit(execute(os.Args[1:], ".", os.Stdout, os.Stderr))
}

func parseArgs(args []string) (cliOptions, error) {
	flags := flag.NewFlagSet("upgrade.sh", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	mode := flags.String("mode", "", "dependency update policy")
	goTarget := flags.String("go-target", "", "exact Go patch version")
	goBinary := flags.String("go-bin", "go", "Go compiler executable")
	reportPath := flags.String("report", defaultReportPath, "JSON report path")
	var k8sTargets repeatedFlag
	flags.Var(&k8sTargets, "k8s-target", "exact Kubernetes module=version target (repeatable)")
	if err := flags.Parse(args); err != nil {
		return cliOptions{}, err
	}
	if flags.NArg() != 0 {
		return cliOptions{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	policy, err := updater.ParsePolicy(*mode, *goTarget, k8sTargets)
	if err != nil {
		return cliOptions{}, err
	}
	return cliOptions{policy: policy, goBinary: *goBinary, reportPath: *reportPath}, nil
}

func execute(args []string, root string, stdout, stderr io.Writer) int {
	options, err := parseArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "invalid invocation: %v\n%s", err, usage)
		return 1
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(stderr, "invalid repository root: %v\n", err)
		return 1
	}
	reportPath := options.reportPath
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(absoluteRoot, reportPath)
	}
	if err := validateRepository(absoluteRoot); err != nil {
		report := updater.Report{Status: "failed", Policy: options.policy, Failure: err.Error(), Modules: []updater.ModuleResult{}}
		if writeErr := updater.WriteReport(reportPath, report); writeErr != nil {
			fmt.Fprintf(stderr, "cannot write report: %v\n", writeErr)
		}
		fmt.Fprintf(stderr, "precondition failed: %v\n", err)
		return updater.ExitCode(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	report, runErr := updater.Run(ctx, updater.Config{Root: absoluteRoot, Policy: options.policy, GoBinary: options.goBinary, Progress: stderr})
	writeErr := updater.WriteReport(reportPath, report)
	if writeErr != nil {
		fmt.Fprintf(stderr, "cannot write report: %v\n", writeErr)
		return 2
	}
	fmt.Fprintf(stdout, "status: %s\nreport: %s\n", report.Status, reportPath)
	if runErr != nil {
		fmt.Fprintf(stderr, "dependency update failed: %s\n", runErr.Error())
		return updater.ExitCode(runErr)
	}
	return 0
}

func validateRepository(root string) error {
	modulePath := ""
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return &updater.PreconditionError{Err: fmt.Errorf("repository root must contain go.mod: %w", err)}
	}
	for _, line := range strings.Split(string(goMod), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			modulePath = fields[1]
			break
		}
	}
	if modulePath != "github.com/openshift/multiarch-tuning-operator" {
		return &updater.PreconditionError{Err: fmt.Errorf("expected repository module github.com/openshift/multiarch-tuning-operator, found %q", modulePath)}
	}
	if _, err := os.Stat(filepath.Join(root, "Makefile")); err != nil {
		return &updater.PreconditionError{Err: fmt.Errorf("repository root must contain Makefile: %w", err)}
	}
	command := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return &updater.PreconditionError{Err: fmt.Errorf("check clean Git working tree: %w: %s", err, strings.TrimSpace(string(output)))}
	}
	if len(strings.TrimSpace(string(output))) != 0 {
		return &updater.PreconditionError{Err: fmt.Errorf("working tree is not clean:\n%s", strings.TrimSpace(string(output)))}
	}
	return nil
}
