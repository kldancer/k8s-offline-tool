package runner

import (
	"io"
	"k8s-offline-tool/pkg/ui"
	"time"
)

// Step 代表一个安装步骤
type Step struct {
	Name   string
	Check  func() (bool, error)
	Action func() error
}

func RunPipeline(steps []Step, prefix string, output io.Writer, dryRun bool) error {
	start := time.Now()
	var err error
	for _, step := range steps {
		if err = runStep(step, prefix, output, dryRun); err != nil {
			break
		}
	}
	ui.PrintPipelineSummary(output, prefix, time.Since(start), err == nil)
	return err
}

func runStep(step Step, prefix string, output io.Writer, dryRun bool) error {
	start := time.Now()

	// 输出增加前缀
	ui.PrintStepStart(output, prefix, step.Name)

	// 1. Check
	ui.PrintCheckStart(output, prefix)
	ok, err := step.Check()
	if err != nil {
		ui.PrintError(output, prefix, err, time.Since(start))
		return err
	}

	if ok {
		ui.PrintSkipped(output, time.Since(start))
		return nil
	}
	ui.PrintToExecute(output)

	if dryRun {
		ui.PrintDryRunSkipped(output, prefix, time.Since(start))
		return nil
	}

	// 2. Action
	ui.PrintActionStart(output, prefix)
	if err := step.Action(); err != nil {
		ui.PrintError(output, prefix, err, time.Since(start))
		return err
	}

	ui.PrintSuccess(output, prefix, time.Since(start))
	return nil
}
