package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"k8s-offline-tool/pkg/config"
	"k8s-offline-tool/pkg/install"
	"log"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "配置文件路径。e.g. config.yaml")
	flag.Parse()

	// 1. 加载配置
	cfg := loadConfig(*cfgPath)
	if err := applyDefaultsAndValidate(cfg); err != nil {
		log.Fatal(err)
		return
	}

	runMode := "安装"
	if cfg.DryRun {
		runMode = "预检查"
	}
	fmt.Printf("开始%s %d 个节点...\n\n", runMode, len(cfg.Nodes))

	results := make([]nodeResult, 0, len(cfg.Nodes))

	// 2. 顺序执行, 先执行master节点安装
	for i := range cfg.Nodes {
		if !cfg.Nodes[i].IsMaster {
			continue
		}
		result := runNode(cfg, i, os.Stdout, cfg.DryRun)
		results = append(results, result)
		if result.Err != nil {
			log.Fatal(result.Err)
			return
		}
	}

	if cfg.InstallMode != config.InstallModeAddonsOnly {
		workerResults := runWorkersSequentially(cfg, os.Stdout, cfg.DryRun)
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

func runNode(cfg *config.Config, i int, writer io.Writer, dryRun bool) nodeResult {
	err := managerRun(cfg, i, writer, dryRun)
	return nodeResult{
		Index:    i,
		IP:       cfg.Nodes[i].IP,
		IsMaster: cfg.Nodes[i].IsMaster,
		Err:      err,
	}
}

func runWorkersSequentially(cfg *config.Config, writer io.Writer, dryRun bool) []nodeResult {
	results := make([]nodeResult, 0, len(cfg.Nodes))
	for i := range cfg.Nodes {
		if cfg.Nodes[i].IsMaster {
			continue
		}
		result := runNode(cfg, i, writer, dryRun)
		results = append(results, result)
	}
	return results
}

func managerRun(cfg *config.Config, i int, writer io.Writer, dryRun bool) error {
	// 创建管理器
	mgr, err := install.NewManager(cfg, &cfg.Nodes[i], writer)
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

func loadConfig(path string) *config.Config {
	// 默认配置
	cfg := &config.Config{
		SSHPort: 22,
		User:    "root",
		Addons: config.AddonsConfig{
			KubeOvn: config.AddonComponentConfig{
				Enabled: false,
				Version: config.KubeOvnVersions[0],
			},
			LocalPathStorage: config.AddonComponentConfig{
				Enabled: false,
				Version: config.LocalPathStorageVersions[0],
			},
			MultusCNI: config.AddonComponentConfig{
				Enabled: false,
				Version: config.MultusCNIVersions[0],
			},
		},
		InstallMode:           config.InstallModeFull,
		CommandTimeoutSeconds: int((600 * time.Second).Seconds()),
		Versions: config.VersionConfig{
			Containerd: config.ContainerdVersions[0],
			Runc:       config.RuncVersions[0],
			Nerdctl:    config.NerdctlVersions[0],
			K8s:        config.K8sVersions[0],
		},
		DryRun: false,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}
	return cfg
}

func stringInSlice(str string, slice []string) bool {
	for _, item := range slice {
		if item == str {
			return true
		}
	}
	return false
}

func applyDefaultsAndValidate(cfg *config.Config) error {
	if len(cfg.Nodes) == 0 {
		return errors.New("Error: No nodes defined in config.yaml")
	}
	if cfg.CommandTimeoutSeconds <= 0 {
		cfg.CommandTimeoutSeconds = int((10 * time.Minute).Seconds())
	}
	if !stringInSlice(cfg.InstallMode, config.SupportedInstallModes) {
		return fmt.Errorf("Error: install_mode %s is not supported.", cfg.InstallMode)
	}

	versions := []struct {
		name      string
		value     *string
		supported []string
	}{
		{"Containerd", &cfg.Versions.Containerd, config.ContainerdVersions},
		{"Runc", &cfg.Versions.Runc, config.RuncVersions},
		{"Nerdctl", &cfg.Versions.Nerdctl, config.NerdctlVersions},
		{"Kubernetes", &cfg.Versions.K8s, config.K8sVersions},
		{"Kube-OVN", &cfg.Addons.KubeOvn.Version, config.KubeOvnVersions},
		{"Multus CNI", &cfg.Addons.MultusCNI.Version, config.MultusCNIVersions},
		{"Local Path Storage", &cfg.Addons.LocalPathStorage.Version, config.LocalPathStorageVersions},
	}

	for _, v := range versions {
		if *v.value == "" {
			*v.value = v.supported[0]
			continue
		}
		if !stringInSlice(*v.value, v.supported) {
			return fmt.Errorf("Error: %s version %s is not supported.", v.name, *v.value)
		}
	}

	for i, node := range cfg.Nodes {
		if strings.TrimSpace(node.IP) == "" {
			return fmt.Errorf("Error: Node[%d] ip is required.", i)
		}
		if strings.TrimSpace(node.Password) == "" {
			return fmt.Errorf("Error: Node[%d] password is required.", i)
		}
	}

	hasMaster := false
	for i := range cfg.Nodes {
		if cfg.Nodes[i].IsMaster {
			hasMaster = true
			break
		}
	}

	if cfg.Registry.Endpoint != "" {
		if cfg.Registry.IP == "" {
			return fmt.Errorf("Error: registry ip is required.")
		}
		if cfg.Registry.Port == 0 {
			return fmt.Errorf("Error: registry port is required.")
		}
		if cfg.Registry.Username == "" || cfg.Registry.Password == "" {
			return fmt.Errorf("Error: registry username and password are required.")
		}
	}

	if !hasMaster && cfg.JoinCommand == "" {
		return fmt.Errorf("Error: join command is required.")
	}

	return nil
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
