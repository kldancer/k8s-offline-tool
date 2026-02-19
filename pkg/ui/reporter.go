package ui

import (
	"fmt"
	"io"
	"time"

	"github.com/fatih/color"
)

var (
	Cyan   = color.New(color.FgCyan).SprintFunc()
	Green  = color.New(color.FgGreen).SprintFunc()
	Yellow = color.New(color.FgYellow).SprintFunc()
	Red    = color.New(color.FgRed).SprintFunc()
)

func PrintStepStart(w io.Writer, prefix, name string) {
	fmt.Fprintf(w, "%s%s %s %s \n", prefix, Cyan("▶ [STEP]"), name, Cyan("…"))
}

func PrintCheckStart(w io.Writer, prefix string) {
	fmt.Fprintf(w, "%s  └─ %s 检查中... ", prefix, Cyan("🔍"))
}

func PrintActionStart(w io.Writer, prefix string) {
	fmt.Fprintf(w, "%s  └─ %s 正在执行...   ", prefix, Cyan("🚀"))
}

func PrintSkipped(w io.Writer, duration time.Duration) {
	fmt.Fprintf(w, "    %s (%v)\n", Green("⏭ 可跳过"), duration.Round(time.Millisecond))
}

func PrintToExecute(w io.Writer) {
	fmt.Fprintf(w, "    %s \n", Yellow("⏳ 待执行"))
}

func PrintDryRunSkipped(w io.Writer, prefix string, duration time.Duration) {
	fmt.Fprintf(w, "%s  └─ %s (%v) \n", prefix, Yellow("⏭ 预检查跳过"), duration.Round(time.Millisecond))
}

func PrintSuccess(w io.Writer, prefix string, duration time.Duration) {
	fmt.Fprintf(w, "%s (%v) \n", Green("✔ 完成"), duration.Round(time.Millisecond))
}

func PrintError(w io.Writer, prefix string, err error, duration time.Duration) {
	fmt.Fprintf(w, "%s (%v) \n", Red("✖ 错误"), duration.Round(time.Second))
	fmt.Fprintf(w, "%s     Error: %v \n", prefix, err)
}

func PrintPipelineSummary(w io.Writer, prefix string, duration time.Duration, success bool) {
	status := Green("成功")
	if !success {
		status = Red("失败")
	}
	fmt.Fprintf(w, "%s%s 所有步骤执行完毕, 结果: %s, 总耗时: %v\n", prefix, Green("✨"), status, duration.Round(time.Second))
}
