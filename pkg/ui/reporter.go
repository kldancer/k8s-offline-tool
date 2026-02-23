package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var (
	Cyan   = color.New(color.FgCyan).SprintFunc()
	Green  = color.New(color.FgGreen).SprintFunc()
	Yellow = color.New(color.FgYellow).SprintFunc()
	Red    = color.New(color.FgRed).SprintFunc()
)

type NodeContext struct {
	IP                string
	Role              string // "Master" or "Worker"
	IsDryRun          bool   // New: to distinguish between install and pre-check
	TotalSteps        int
	CurrentStep       int
	CurrentStepName   string
	CurrentStepStatus string // New: for dynamic TUI status
	ResourceProgress  string // New: for real-time resource distribution
	Bar               *mpb.Bar
	LogBuffer         *bytes.Buffer
	StartTime         time.Time
	Duration          time.Duration
	Err               error
	Success           bool
	Mu                sync.Mutex
}

func NewNodeContext(ip, role string, totalSteps int, isDryRun bool) *NodeContext {
	return &NodeContext{
		IP:         ip,
		Role:       role,
		IsDryRun:   isDryRun,
		TotalSteps: totalSteps,
		LogBuffer:  new(bytes.Buffer),
	}
}

func (n *NodeContext) SetBar(bar *mpb.Bar) {
	n.Bar = bar
}

func (n *NodeContext) UpdateStatus(status string) {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	n.CurrentStepStatus = status
}

func (n *NodeContext) UpdateResourceProgress(progress string) {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	n.ResourceProgress = progress
}

func (n *NodeContext) StartStep(name string) {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	n.CurrentStep++
	n.CurrentStepName = name
	n.CurrentStepStatus = Cyan("🔍 检查中...")
	n.ResourceProgress = ""
}

func (n *NodeContext) EndStep(err error, duration time.Duration, extraStatus string) {
	n.Mu.Lock()
	defer n.Mu.Unlock()

	prefix := fmt.Sprintf("[%s] ", n.IP)
	stepName := n.CurrentStepName
	// 40 display width should be enough for most Chinese step names
	paddedName := runewidth.FillRight(stepName, 40)

	if err != nil {
		n.Err = err
		n.CurrentStepStatus = Red("✖ 错误")
		// Align status for error
		paddedStatus := runewidth.FillRight(Red("✖ 错误"), 15)
		fmt.Fprintf(n.LogBuffer, "%s%s %s %s (%v)\n", prefix, Cyan("▶ [STEP]"), paddedName, paddedStatus, duration.Round(time.Millisecond))
		fmt.Fprintf(n.LogBuffer, "%s     %s: %v\n", prefix, Red("Error"), err)
	} else {
		status := Green("✔ 完成")
		if extraStatus != "" {
			status = extraStatus
		}
		n.CurrentStepStatus = status
		// Align status for success/skipped
		paddedStatus := runewidth.FillRight(status, 15)
		fmt.Fprintf(n.LogBuffer, "%s%s %s %s (%v)\n", prefix, Cyan("▶ [STEP]"), paddedName, paddedStatus, duration.Round(time.Millisecond))
		if n.Bar != nil {
			n.Bar.Increment()
		}
	}
}

func (n *NodeContext) Finish(success bool, duration time.Duration) {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	n.Success = success
	n.Duration = duration

	prefix := fmt.Sprintf("[%s] ", n.IP)
	statusStr := Green("成功")
	if !success {
		statusStr = Red("失败")
	}
	opName := "步骤执行"
	if n.IsDryRun {
		opName = "预检查"
	}
	fmt.Fprintf(n.LogBuffer, "%s%s 所有%s完毕, 结果: %s, 总耗时: %v\n", prefix, Green("✨"), opName, statusStr, duration.Round(time.Second))
	if !success && n.Err != nil {
		fmt.Fprintf(n.LogBuffer, "%s     %s: %v\n", prefix, Red("原因"), n.Err)
	}

	if n.Bar != nil {
		n.Bar.Abort(false)
	}
}

func (n *NodeContext) Write(p []byte) (int, error) {
	return n.LogBuffer.Write(p)
}

func SetupTUI(nodes []*NodeContext) (*mpb.Progress, func()) {
	p := mpb.New(mpb.WithWidth(40))
	var headerBars []*mpb.Bar

	emptyFiller := mpb.BarFillerFunc(func(w io.Writer, _ decor.Statistics) error {
		return nil
	})

	addHeader := func(role string) {
		count := 0
		for _, n := range nodes {
			if n.Role == role {
				count++
			}
		}
		if count == 0 {
			return
		}

		bar := p.MustAdd(0, emptyFiller,
			mpb.PrependDecorators(
				decor.Any(func(s decor.Statistics) string {
					total := 0
					running := 0
					completed := 0
					for _, n := range nodes {
						if n.Role == role {
							total++
							n.Mu.Lock()
							if n.Success || n.Err != nil {
								completed++
							} else if n.CurrentStep > 0 {
								running++
							}
							n.Mu.Unlock()
						}
					}
					icon := "📦"
					if role == "Worker" {
						icon = "💻"
					}
					return fmt.Sprintf("%s %s 节点组 [%d/%d 运行中, %d 完成]", icon, role, running, total, completed)
				}),
			),
		)
		headerBars = append(headerBars, bar)
	}

	// 1. Master Group
	addHeader("Master")
	for _, node := range nodes {
		if node.Role == "Master" {
			addNodeBar(p, node)
		}
	}

	// 2. Worker Group
	addHeader("Worker")
	for _, node := range nodes {
		if node.Role == "Worker" {
			addNodeBar(p, node)
		}
	}

	return p, func() {
		for _, b := range headerBars {
			b.Abort(false)
		}
		p.Wait()
	}
}

func addNodeBar(p *mpb.Progress, node *NodeContext) {
	name := fmt.Sprintf("[%s]", node.IP)

	statusDecorator := decor.Any(func(s decor.Statistics) string {
		node.Mu.Lock()
		defer node.Mu.Unlock()

		if node.Err != nil {
			if node.CurrentStepName == "" {
				return Red(fmt.Sprintf("✖ 失败: %v", node.Err))
			}
			return Red(fmt.Sprintf("✖ 失败: [%s]", node.CurrentStepName))
		}

		if node.Success {
			if node.IsDryRun {
				return Green(fmt.Sprintf("✔ 预检查完成 (%v)", node.Duration.Round(time.Second)))
			}
			return Green(fmt.Sprintf("✔ 安装成功 (%v)", node.Duration.Round(time.Second)))
		}

		if node.CurrentStep == 0 {
			return Yellow("⏳ 等待执行...")
		}

		status := node.CurrentStepStatus
		if node.ResourceProgress != "" {
			status = fmt.Sprintf("🚀 %s", node.ResourceProgress)
		}
		return fmt.Sprintf("⏳ [%02d/%02d] %s: %s", node.CurrentStep, node.TotalSteps, node.CurrentStepName, status)
	})

	bar := p.MustAdd(int64(node.TotalSteps),
		mpb.BarStyle().Build(),
		mpb.PrependDecorators(
			decor.Name(name, decor.WC{W: 16, C: decor.DindentRight | decor.DSyncWidth}),
			decor.Percentage(decor.WCSyncWidth),
		),
		mpb.AppendDecorators(
			decor.Name(" "),
			statusDecorator,
		),
	)
	node.SetBar(bar)
}

func GenerateFinalReport(nodes []*NodeContext, reportPath string) error {
	file, err := os.Create(reportPath)
	if err != nil {
		return err
	}
	defer file.Close()

	file.WriteString("================ K8s 离线安装详细报告 ================\n\n")

	// 1. 优先输出 Master 节点
	file.WriteString("📦 [Master 节点组执行历史]\n")
	for _, node := range nodes {
		if node.Role == "Master" {
			file.WriteString(fmt.Sprintf("---------------- %s ----------------\n", node.IP))
			file.Write(node.LogBuffer.Bytes())
			file.WriteString("\n")
		}
	}

	// 2. 然后输出 Worker 节点
	file.WriteString("💻 [Worker 节点组执行历史]\n")
	for _, node := range nodes {
		if node.Role == "Worker" {
			file.WriteString(fmt.Sprintf("---------------- %s ----------------\n", node.IP))
			file.Write(node.LogBuffer.Bytes())
			file.WriteString("\n")
		}
	}

	return nil
}

// Obsolete step printing functions removed
