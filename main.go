package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sync"

	"k8s-offline-tool/pkg/config"
	"k8s-offline-tool/pkg/install"

	"gopkg.in/yaml.v3"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "Path to configuration file")
	
	flag.Parse()

	// 1. 加载配置
	cfg := loadConfig(*cfgPath)
	if len(cfg.Nodes) == 0 {
		log.Fatal("Error: No nodes defined in config.yaml")
	}

	if cfg.ConcurrentExec == true {
	}

	// 判断安装版本
	if cfg.Versions.Containerd == "" {
		cfg.Versions.Containerd = config.ContainerdVersions[0]
	} else {
		if !stringInSlice(cfg.Versions.Containerd, config.ContainerdVersions) {
			log.Fatalf("Error: Containerd version %s is not supported.", cfg.Versions.Containerd)
			return
		}
	}

	if cfg.Versions.Runc == "" {
		cfg.Versions.Runc = config.RuncVersions[0]
	} else {
		if cfg.Versions.Runc != "" && !stringInSlice(cfg.Versions.Runc, config.RuncVersions) {
			log.Fatalf("Error: Runc version %s is not supported.", cfg.Versions.Runc)
			return
		}
	}

	if cfg.Versions.Nerdctl == "" {
		cfg.Versions.Nerdctl = config.NerdctlVersions[0]
	} else {
		if !stringInSlice(cfg.Versions.Nerdctl, config.NerdctlVersions) {
			log.Fatalf("Error: Nerdctl version %s is not supported.", cfg.Versions.Nerdctl)
			return
		}
	}

	if cfg.Versions.K8s == "" {
		cfg.Versions.K8s = config.K8sVersions[0]
	} else {
		if !stringInSlice(cfg.Versions.K8s, config.K8sVersions) {
			log.Fatalf("Error: Kubernetes version %s is not supported.", cfg.Versions.K8s)
			return
		}
	}

	fmt.Printf("Starting parallel deployment on %d nodes...\n\n", len(cfg.Nodes))

	// 2. 并行执行
	var wg sync.WaitGroup
	errChan := make(chan error, len(cfg.Nodes))

	for i := range cfg.Nodes {
		wg.Add(1)
		node := cfg.Nodes[i] // 复制一份，避免闭包问题

		go func(n config.NodeConfig) {
			defer wg.Done()

			// 创建管理器
			mgr, err := install.NewManager(cfg, &n)
			if err != nil {
				errChan <- fmt.Errorf("[%s] Init failed: %v", n.IP, err)
				return
			}
			defer mgr.Close()

			// 执行安装
			if err := mgr.Run(); err != nil {
				errChan <- fmt.Errorf("[%s] Failed: %v", n.IP, err)
				return
			}
		}(node)
	}

	// 3. 等待所有结束
	wg.Wait()
	close(errChan)

	// 4. 汇总结果
	failed := false
	for err := range errChan {
		log.Printf("ERROR: %v", err)
		failed = true
	}

	if !failed {
		fmt.Println("\nAll nodes completed successfully!")
	} else {
		fmt.Println("\nSome nodes failed. Check logs above.")
		os.Exit(1)
	}
}

func loadConfig(path string) *config.Config {
	cfg := &config.Config{SSHPort: 22, User: "root"}
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
