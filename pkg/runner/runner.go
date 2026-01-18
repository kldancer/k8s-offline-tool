package runner

import (
	"fmt"
	"time"

	"github.com/fatih/color"
)

// Step 代表一个安装步骤
type Step struct {
	Name   string
	Check  func() (bool, error)
	Action func() error
}

func RunPipeline(steps []Step, prefix string) error {
	for _, step := range steps {
		if err := runStep(step, prefix); err != nil {
			return err
		}
	}
	return nil
}

func runStep(step Step, prefix string) error {
	start := time.Now()

	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	white := color.New(color.FgWhite).SprintFunc()

	// 输出增加前缀
	fmt.Printf("%s%s %s ...\n", prefix, cyan("[STEP]"), white(step.Name))

	// 1. Check
	fmt.Printf("%s  └─ 检查中... ", prefix)
	ok, err := step.Check()
	if err != nil {
		fmt.Printf("%s\n", red("错误"))
		fmt.Printf("%s     Error: %v\n", prefix, err)
		return err
	}

	if ok {
		fmt.Printf("%s\n", green("跳过"))
		return nil
	}
	fmt.Printf("%s\n", yellow("待执行"))

	// 2. Action
	fmt.Printf("%s  └─ 正在执行...   ", prefix)
	if err := step.Action(); err != nil {
		fmt.Printf("%s (%v)\n", red("错误"), time.Since(start).Round(time.Second))
		fmt.Printf("%s     Error: %v\n", prefix, err)
		return err
	}

	fmt.Printf("%s %s (%v)\n", green("完成"), prefix, time.Since(start).Round(time.Millisecond))
	return nil
}
