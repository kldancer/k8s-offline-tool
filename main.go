package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"k8s-offline-tool/pkg/config"
	"k8s-offline-tool/pkg/install"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

func main() {
	cfgPath := flag.String("config", "", "配置文件路径。e.g. config.yaml")
	dryRun := flag.Bool("dry-run", false, "仅执行预检查，不执行安装动作")
	flag.Parse()

	// 1. 加载配置
	cfg := loadConfig(*cfgPath)
	if err := applyDefaultsAndValidate(cfg); err != nil {
		log.Fatal(err)
		return
	}

	runMode := "安装"
	if *dryRun {
		runMode = "预检查"
	}
	fmt.Printf("开始%s %d 个节点...\n\n", runMode, len(cfg.Nodes))

	results := make([]nodeResult, 0, len(cfg.Nodes))
	var runErr error

	// 2. 顺序执行, 先执行master节点安装
	for i := range cfg.Nodes {
		if !cfg.Nodes[i].IsMaster {
			continue
		}
		result := runNode(cfg, i, os.Stdout, *dryRun)
		results = append(results, result)
		if result.Err != nil && runErr == nil {
			runErr = result.Err
		}
	}

	workerResults, err := runWorkersConcurrently(cfg, *dryRun)
	if err != nil && runErr == nil {
		runErr = err
	}
	results = append(results, workerResults...)

	printSummary(results, *dryRun)
	if runErr != nil {
		log.Fatal(runErr)
	}
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

type workerResult struct {
	result nodeResult
	logs   *bytes.Buffer
}

func runWorkersConcurrently(cfg *config.Config, dryRun bool) ([]nodeResult, error) {
	var (
		wg      sync.WaitGroup
		results = make(chan workerResult, len(cfg.Nodes))
	)

	for i := range cfg.Nodes {
		if cfg.Nodes[i].IsMaster {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			buf := &bytes.Buffer{}
			err := managerRun(cfg, idx, buf, dryRun)
			results <- workerResult{
				result: nodeResult{
					Index:    idx,
					IP:       cfg.Nodes[idx].IP,
					IsMaster: cfg.Nodes[idx].IsMaster,
					Err:      err,
				},
				logs: buf,
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	ordered := make([]workerResult, 0)
	for res := range results {
		ordered = append(ordered, res)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].result.Index < ordered[j].result.Index
	})

	finalResults := make([]nodeResult, 0, len(ordered))
	var firstErr error
	for _, res := range ordered {
		fmt.Print(res.logs.String())
		finalResults = append(finalResults, res.result)
		if res.result.Err != nil && firstErr == nil {
			firstErr = res.result.Err
		}
	}

	return finalResults, firstErr
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
	cfg := &config.Config{SSHPort: 22, User: "root"}
	if strings.TrimSpace(path) == "" {
		path = "config.yaml"
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

	versions := []struct {
		name      string
		value     *string
		supported []string
	}{
		{"Containerd", &cfg.Versions.Containerd, config.ContainerdVersions},
		{"Runc", &cfg.Versions.Runc, config.RuncVersions},
		{"Nerdctl", &cfg.Versions.Nerdctl, config.NerdctlVersions},
		{"Kubernetes", &cfg.Versions.K8s, config.K8sVersions},
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
