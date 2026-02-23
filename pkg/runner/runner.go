package runner

import (
	"k8s-offline-tool/pkg/ui"
	"time"
)

// Step 代表一个安装步骤
type Step struct {
	Name   string
	Check  func() (bool, error)
	Action func() error
}

func RunPipeline(steps []Step, prefix string, nodeCtx *ui.NodeContext, dryRun bool) error {
	var err error
	for _, step := range steps {
		if err = runStep(step, prefix, nodeCtx, dryRun); err != nil {
			break
		}
	}

	return err
}

func runStep(step Step, prefix string, nodeCtx *ui.NodeContext, dryRun bool) error {
	start := time.Now()

	nodeCtx.StartStep(step.Name)

	// 1. Check
	nodeCtx.UpdateStatus(ui.Cyan("🔍 检查中..."))
	ok, err := step.Check()
	if err != nil {
		nodeCtx.EndStep(err, time.Since(start), "")
		return err
	}

	if ok {
		nodeCtx.UpdateStatus(ui.Green("⏭ 可跳过"))
		nodeCtx.EndStep(nil, time.Since(start), ui.Green("⏭ 可跳过"))
		return nil
	}
	nodeCtx.UpdateStatus(ui.Yellow("⏳ 待执行"))

	if dryRun {
		nodeCtx.UpdateStatus(ui.Yellow("⏭ 预检查跳过"))
		nodeCtx.EndStep(nil, time.Since(start), ui.Yellow("⏭ 预检查跳过"))
		return nil
	}

	// 2. Action
	nodeCtx.UpdateStatus(ui.Cyan("🚀 正在执行..."))
	if err := step.Action(); err != nil {
		nodeCtx.EndStep(err, time.Since(start), "")
		return err
	}

	nodeCtx.EndStep(nil, time.Since(start), "")
	return nil
}
