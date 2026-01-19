package main

import (
	"flag"
	"fmt"
	"k8s-offline-tool/pkg/config"
	"k8s-offline-tool/pkg/install"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	cfgPath := flag.String("config", "", "配置文件路径。e.g. config.yaml")
	flag.Parse()

	// 1. 加载配置
	cfg := loadConfig(*cfgPath)
	if len(cfg.Nodes) == 0 {
		log.Fatal("Error: No nodes defined in config.yaml")
		return
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

	fmt.Printf("开始部署 %d 个节点...\n\n", len(cfg.Nodes))

	// 2. 顺序执行, 先执行master节点安装
	for i := range cfg.Nodes {
		if !cfg.Nodes[i].IsMaster {
			continue
		}
		if err := managerRun(cfg, i); err != nil {
			log.Fatal(err)
			return
		}
	}

	for i := range cfg.Nodes {
		if cfg.Nodes[i].IsMaster {
			continue
		}
		if err := managerRun(cfg, i); err != nil {
			log.Fatal(err)
			return
		}
	}
}

func managerRun(cfg *config.Config, i int) error {
	// 创建管理器
	mgr, err := install.NewManager(cfg, &cfg.Nodes[i])
	if err != nil {
		return fmt.Errorf("[%s] Init failed: %v", cfg.Nodes[i].IP, err)
	}
	defer mgr.Close()

	// 执行安装
	if err := mgr.Run(); err != nil {
		return fmt.Errorf("[%s] Failed: %v", cfg.Nodes[i].IP, err)
	}
	return nil
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
