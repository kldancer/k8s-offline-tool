package runner

import (
	"fmt"
	"io"
	"time"

	"github.com/fatih/color"
)

// Step 代表一个安装步骤
type Step struct {
	Name   string
	Check  func() (bool, error)
	Action func() error
}

func RunPipeline(steps []Step, prefix string, output io.Writer, dryRun bool) error {
	for _, step := range steps {
		if err := runStep(step, prefix, output, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func runStep(step Step, prefix string, output io.Writer, dryRun bool) error {
	start := time.Now()

	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	// 输出增加前缀
	fmt.Fprintf(output, "%s%s %s %s ...\n", prefix, cyan("▶ [STEP]"), step.Name, cyan("…"))

	// 1. Check
	fmt.Fprintf(output, "%s  └─ %s 检查中... ", prefix, cyan("🔍"))
	ok, err := step.Check()
	if err != nil {
		fmt.Fprintf(output, "%s\n", red("✖ 错误"))
		fmt.Fprintf(output, "%s     Error: %v\n", prefix, err)
		return err
	}

	if ok {
		fmt.Fprintf(output, "%s\n", green("⏭ 可跳过"))
		return nil
	}
	fmt.Fprintf(output, "%s\n", yellow("⏳ 待执行"))

	if dryRun {
		fmt.Fprintf(output, "%s  └─ %s (%v)\n", prefix, yellow("⏭ 预检查跳过"), time.Since(start).Round(time.Millisecond))
		return nil
	}

	// 2. Action
	fmt.Fprintf(output, "%s  └─ %s 正在执行...   ", prefix, cyan("🚀"))
	if err := step.Action(); err != nil {
		fmt.Fprintf(output, "%s (%v)\n", red("✖ 错误"), time.Since(start).Round(time.Second))
		fmt.Fprintf(output, "%s     Error: %v\n", prefix, err)
		return err
	}

	fmt.Fprintf(output, "%s %s (%v)\n", green("✔ 完成"), prefix, time.Since(start).Round(time.Millisecond))
	return nil
}
