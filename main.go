package main

import (
	"flag"
	"fmt"
	"io"
	"k8s-offline-tool/pkg/config"
	"k8s-offline-tool/pkg/install"
	"log"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

func main() {
	cfgPath := flag.String("config", "example/config-ha.yaml", "配置文件路径。e.g. config.yaml")
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
	fmt.Printf("开始%s %d 个节点...\n\n", runMode, len(cfg.Nodes))

	results := make([]nodeResult, 0, len(cfg.Nodes))
	runIndex := 0

	// 2. 顺序执行, 先执行master节点安装
	for _, i := range masterNodeOrder(cfg) {
		runIndex++
		result := runNode(cfg, i, runIndex, len(cfg.Nodes), os.Stdout, cfg.DryRun)
		results = append(results, result)
		if result.Err != nil {
			log.Fatal(result.Err)
			return
		}
	}

	if cfg.InstallMode != config.InstallModeAddonsOnly {
		workerResults := runWorkersSequentially(cfg, &runIndex, os.Stdout, cfg.DryRun)
		results = append(results, workerResults...)
		for _, result := range workerResults {
			if result.Err != nil {
				log.Fatal(result.Err)
				return
			}
		}
	}

	printSummary(results, cfg.DryRun)
}

type nodeResult struct {
	Index    int
	IP       string
	IsMaster bool
	Err      error
}

func runNode(cfg *config.Config, i int, runIndex int, totalNodes int, writer io.Writer, dryRun bool) nodeResult {
	err := managerRun(cfg, i, runIndex, totalNodes, writer, dryRun)
	return nodeResult{
		Index:    i,
		IP:       cfg.Nodes[i].IP,
		IsMaster: cfg.Nodes[i].IsMaster,
		Err:      err,
	}
}

func runWorkersSequentially(cfg *config.Config, runIndex *int, writer io.Writer, dryRun bool) []nodeResult {
	results := make([]nodeResult, 0, len(cfg.Nodes))
	for i := range cfg.Nodes {
		if cfg.Nodes[i].IsMaster {
			continue
		}
		*runIndex = *runIndex + 1
		result := runNode(cfg, i, *runIndex, len(cfg.Nodes), writer, dryRun)
		results = append(results, result)
	}
	return results
}

func managerRun(cfg *config.Config, i int, runIndex int, totalNodes int, writer io.Writer, dryRun bool) error {
	// 创建管理器
	mgr, err := install.NewManager(cfg, &cfg.Nodes[i], runIndex, totalNodes, writer)
	if err != nil {
		return fmt.Errorf("[%s] Init failed: %v", cfg.Nodes[i].IP, err)
	}
	defer mgr.Close()

	// 执行安装
	if err := mgr.Run(dryRun); err != nil {
		return fmt.Errorf("[%s] Failed: %v", cfg.Nodes[i].IP, err)
	}
	return nil
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

func printSummary(results []nodeResult, dryRun bool) {
	if len(results) == 0 {
		return
	}
	action := "安装"
	if dryRun {
		action = "预检查"
	}
	fmt.Printf("\n%s结果汇总:\n", action)
	for _, result := range results {
		role := "Worker"
		if result.IsMaster {
			role = "Master"
		}
		status := "成功"
		if result.Err != nil {
			status = "失败"
		}
		line := fmt.Sprintf(" - %s (%s): %s", result.IP, role, status)
		if result.Err != nil {
			line = fmt.Sprintf("%s (%v)", line, result.Err)
		}
		fmt.Println(line)
	}
}
