package main

import (
	"flag"
	"fmt"
	"k8s-offline-tool/pkg/config"
	"k8s-offline-tool/pkg/install"
	"k8s-offline-tool/pkg/ui"
	"log"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

func main() {
	cfgPath := flag.String("config", "example/config-ola.yaml", "配置文件路径。e.g. config.yaml")
	reportPath := flag.String("report", "k8s-install-summary.log", "安装报告生成路径")
	flag.Parse()

	// 1. 加载配置
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := config.ApplyDefaultsAndValidate(cfg); err != nil {
		log.Fatal(err)
		return
	}

	runMode := "安装"
	if cfg.DryRun {
		runMode = "预检查"
	}

	if cfg.InstallMode == config.InstallModeAddonsOnly {
		fmt.Printf("安装插件模式...\n")
	} else {
		fmt.Printf("开始%s %d 个节点...\n\n", runMode, len(cfg.Nodes))
	}

	// 2. 区分角色
	masterIndices := masterNodeOrder(cfg)
	workerIndices := []int{}
	if cfg.InstallMode != config.InstallModeAddonsOnly {
		for i := range cfg.Nodes {
			isMaster := false
			for _, mIdx := range masterIndices {
				if mIdx == i {
					isMaster = true
					break
				}
			}
			if !isMaster {
				workerIndices = append(workerIndices, i)
			}
		}
	}

	// 3. 初始化 Context
	masterContexts := make([]*ui.NodeContext, len(masterIndices))
	for i, idx := range masterIndices {
		masterContexts[i] = ui.NewNodeContext(cfg.Nodes[idx].IP, "Master", 0, cfg.DryRun)
	}

	workerContexts := make([]*ui.NodeContext, len(workerIndices))
	for i, idx := range workerIndices {
		workerContexts[i] = ui.NewNodeContext(cfg.Nodes[idx].IP, "Worker", 0, cfg.DryRun)
	}

	// 4. 设置 TUI
	allContexts := append(masterContexts, workerContexts...)
	_, waitTUI := ui.SetupTUI(allContexts)

	// 5. 执行 Master (顺序)
	masterHasErr := false
	for i, idx := range masterIndices {
		ctx := masterContexts[i]
		mgr, err := install.NewManager(cfg, &cfg.Nodes[idx], i+1, len(cfg.Nodes), ctx)
		if err != nil {
			ctx.Mu.Lock()
			ctx.Err = fmt.Errorf("ssh 连接失败: %v", err)
			ctx.Mu.Unlock()
			ctx.Finish(false, 0)
			masterHasErr = true
			break
		}
		if err = mgr.Run(ctx, cfg.DryRun); err != nil {
			masterHasErr = true
			mgr.Close()
			break
		}
		mgr.Close()
	}

	// 6. 执行 Worker (并发)

	if !masterHasErr {
		var wg sync.WaitGroup
		for i, idx := range workerIndices {
			wg.Add(1)
			go func(nodeIdx int, ctx *ui.NodeContext, runIdx int) {
				defer wg.Done()
				mgr, err := install.NewManager(cfg, &cfg.Nodes[nodeIdx], runIdx, len(cfg.Nodes), ctx)
				if err != nil {
					ctx.Mu.Lock()
					ctx.Err = fmt.Errorf("ssh 连接失败: %v", err)
					ctx.Mu.Unlock()
					ctx.Finish(false, 0)
					return
				}
				defer mgr.Close()
				_ = mgr.Run(ctx, cfg.DryRun)
			}(idx, workerContexts[i], len(masterIndices)+i+1)
		}
		wg.Wait()
	} else {
		// 如果 Master 失败，标记所有未开始的节点为已跳过/失败，以解除 TUI 阻塞
		for _, ctx := range allContexts {
			ctx.Mu.Lock()
			if !ctx.Success && ctx.Err == nil {
				ctx.Err = fmt.Errorf("因前序 Master 节点执行失败而跳过")
				ctx.Mu.Unlock()
				ctx.Finish(false, 0)
			} else {
				ctx.Mu.Unlock()
			}
		}
	}

	// 7. 结束 TUI
	waitTUI()

	// 8. 生成最终报告
	if err := ui.GenerateFinalReport(allContexts, *reportPath); err != nil {
		fmt.Printf("\n生成报告失败: %v\n", err)
	} else {
		fmt.Printf("\n✨ %s结束！各节点详细步骤日志已生成并分类排序: %s\n", runMode, *reportPath)
	}

	// 9. 打印简要汇总
	printSummaryFromContexts(allContexts, cfg.DryRun)
}

func printSummaryFromContexts(contexts []*ui.NodeContext, dryRun bool) {
	if len(contexts) == 0 {
		return
	}
	action := "安装"
	if dryRun {
		action = "预检查"
	}
	fmt.Printf("\n%s结果汇总:\n", action)
	for _, ctx := range contexts {
		status := ui.Green("成功")
		if !ctx.Success {
			status = ui.Red("失败")
		}
		line := fmt.Sprintf(" - %s (%s): %s", ctx.IP, ctx.Role, status)
		if ctx.Err != nil {
			line = fmt.Sprintf("%s (%v)", line, ctx.Err)
		}
		fmt.Println(line)
	}
}

func masterNodeOrder(cfg *config.Config) []int {
	masters := make([]int, 0, len(cfg.Nodes))
	primaryIndex := -1
	for i := range cfg.Nodes {
		if !cfg.Nodes[i].IsMaster {
			continue
		}
		masters = append(masters, i)
		if cfg.Nodes[i].IsPrimaryMaster {
			primaryIndex = i
		}
	}
	if !cfg.HA.Enabled || primaryIndex == -1 {
		return masters
	}

	// addons 模式只需要主master节点执行安装
	if cfg.InstallMode == config.InstallModeAddonsOnly {
		return []int{primaryIndex}
	}

	ordered := make([]int, 0, len(masters))
	ordered = append(ordered, primaryIndex)
	for _, idx := range masters {
		if idx == primaryIndex {
			continue
		}
		ordered = append(ordered, idx)
	}
	return ordered
}

func loadConfig(path string) (*config.Config, error) {
	// 默认配置
	cfg := &config.Config{
		SSHPort: 22,
		User:    "root",
		Addons: config.AddonsConfig{
			KubeOvn: config.AddonComponentConfig{
				Enabled: false,
				Version: config.KubeOvnVersions[0],
			},
			MultusCNI: config.AddonComponentConfig{
				Enabled: false,
				Version: config.MultusCNIVersions[0],
			},
			Hami: config.AddonComponentConfig{
				Enabled: false,
				Version: config.HamiVersions[0],
			},
			KubePrometheus: config.AddonComponentConfig{
				Enabled: false,
				Version: config.KubePrometheusVersions[0],
			},
		},
		InstallMode:           config.InstallModeFull,
		CommandTimeoutSeconds: int((600 * time.Second).Seconds()),
		Versions: config.VersionConfig{
			DockerCE:   config.DockerCEVersions[0],
			Containerd: config.ContainerdVersions[0],
			Runc:       config.RuncVersions[0],
			Nerdctl:    config.NerdctlVersions[0],
			K8s:        config.K8sVersions[0],
		},
		DryRun: false,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	return cfg, nil
}
