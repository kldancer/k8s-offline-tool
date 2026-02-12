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
	fmt.Fprintf(w, "%s%s %s %s ...", prefix, Cyan("▶ [STEP]"), name, Cyan("…"))
}

func PrintCheckStart(w io.Writer, prefix string) {
	fmt.Fprintf(w, "%s  └─ %s 检查中... ", prefix, Cyan("🔍"))
}

func PrintActionStart(w io.Writer, prefix string) {
	fmt.Fprintf(w, "%s  └─ %s 正在执行...   ", prefix, Cyan("🚀"))
}

func PrintSkipped(w io.Writer) {
	fmt.Fprintf(w, "%s", Green("⏭ 可跳过"))
}

func PrintToExecute(w io.Writer) {
	fmt.Fprintf(w, "%s", Yellow("⏳ 待执行"))
}

func PrintDryRunSkipped(w io.Writer, prefix string, duration time.Duration) {
	fmt.Fprintf(w, "%s  └─ %s (%v)", prefix, Yellow("⏭ 预检查跳过"), duration.Round(time.Millisecond))
}

func PrintSuccess(w io.Writer, prefix string, duration time.Duration) {
	fmt.Fprintf(w, "%s %s (%v)", Green("✔ 完成"), prefix, duration.Round(time.Millisecond))
}

func PrintError(w io.Writer, prefix string, err error, duration time.Duration) {
	fmt.Fprintf(w, "%s (%v)", Red("✖ 错误"), duration.Round(time.Second))
	fmt.Fprintf(w, "%s     Error: %v", prefix, err)
}
