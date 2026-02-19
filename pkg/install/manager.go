package install

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"k8s-offline-tool/pkg/config"
	"k8s-offline-tool/pkg/install/strategy"
	"k8s-offline-tool/pkg/runner"
	"k8s-offline-tool/pkg/ssh"
)

// Manager 现在对应一个节点的安装任务
type Manager struct {
	globalCfg  *config.Config
	nodeCfg    *config.NodeConfig
	client     *ssh.Client
	installer  strategy.NodeInstaller
	context    *strategy.Context
	output     io.Writer
	nodeIndex  int
	totalNodes int
}

func (m *Manager) calculateLocalHash() (string, error) {
	f, err := os.Open(m.globalCfg.ResourcePackage)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// NewManager 创建针对特定节点的管理器
func NewManager(globalCfg *config.Config, nodeCfg *config.NodeConfig, nodeIndex int, totalNodes int, output io.Writer) (*Manager, error) {
	if output == nil {
		output = os.Stdout
	}
	user := globalCfg.User
	port := nodeCfg.SSHPort
	if port == 0 {
		port = globalCfg.SSHPort
	}

	commandTimeout := time.Duration(globalCfg.CommandTimeoutSeconds) * time.Second
	client, err := ssh.NewClient(nodeCfg.IP, port, user, nodeCfg.Password, commandTimeout)
	if err != nil {
		return nil, fmt.Errorf("ssh connection to %s failed: %v", nodeCfg.IP, err)
	}

	return &Manager{
		globalCfg:  globalCfg,
		nodeCfg:    nodeCfg,
		client:     client,
		output:     output,
		nodeIndex:  nodeIndex,
		totalNodes: totalNodes,
	}, nil
}

func (m *Manager) Close() {
	if m.client != nil {
		m.client.Close()
	}
}

func (m *Manager) detectEnv() error {
	arch, err := m.client.DetectArch()
	if err != nil {
		return fmt.Errorf("failed to detect arch: %v", err)
	}

	// 合并探测命令以减少 RTT
	// 输出格式: NAME|VERSION_ID|KERNEL|HAS_GPU|HAS_NPU
	probeCmd := `
name=$(grep '^NAME=' /etc/os-release | cut -d= -f2 | sed 's/"//g')
version=$(grep '^VERSION_ID=' /etc/os-release | cut -d= -f2 | sed 's/"//g')
kernel=$(uname -r)
gpu="false"
if lspci | grep -i nvidia >/dev/null 2>&1; then gpu="true"; fi
npu="false"
if lspci | grep -i "Huawei" >/dev/null 2>&1; then npu="true"; fi
echo "${name}|${version}|${kernel}|${gpu}|${npu}"
`
	out, err := m.client.RunCommand(strings.TrimSpace(probeCmd))
	if err != nil {
		return fmt.Errorf("failed to probe environment: %v", err)
	}

	parts := strings.Split(strings.TrimSpace(out), "|")
	if len(parts) != 5 {
		return fmt.Errorf("unexpected probe output: %s", out)
	}

	systemName := parts[0]
	systemVersion := parts[1]
	kernelVersion := parts[2]
	hasGPU := parts[3] == "true"
	hasNPU := parts[4] == "true"

	m.context = &strategy.Context{
		Cfg:           m.globalCfg,
		Arch:          arch,
		SystemName:    systemName,
		SystemVersion: systemVersion,
		KernelVersion: kernelVersion,
		HasGPU:        hasGPU,
		HasNPU:        hasNPU,
		RemoteTmpDir:  config.RemoteTmpDir,
		RunCmd:        m.client.RunCommand,
	}

	osInfo := strings.ToLower(systemName)
	if strings.Contains(osInfo, "fedora") || strings.Contains(osInfo, "centos") {
		m.installer = &strategy.FedoraInstaller{Ctx: m.context}
	} else if strings.Contains(osInfo, "ubuntu") || strings.Contains(osInfo, "debian") {
		m.installer = &strategy.UbuntuInstaller{Ctx: m.context}
	} else if strings.Contains(osInfo, "openeuler") {
		m.installer = &strategy.OpenEulerInstaller{Ctx: m.context}
	} else {
		return fmt.Errorf("unsupported OS: %s", osInfo)
	}
	return nil
}

func (m *Manager) distributeResources() error {
	localHash, err := m.calculateLocalHash()
	if err != nil {
		return fmt.Errorf("failed to calculate local resource hash: %v", err)
	}

	remotePkgPath := path.Join(m.context.RemoteTmpDir, "resources.tar.gz")
	remoteMarkerPath := path.Join(m.context.RemoteTmpDir, ".extracted_success")

	// 上传压缩包
	prefix := fmt.Sprintf("[%s] ", m.nodeCfg.IP)
	fileName := filepath.Base(m.globalCfg.ResourcePackage)

	f, err := os.Open(m.globalCfg.ResourcePackage)
	if err != nil {
		return err
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		return err
	}
	totalSize := fileInfo.Size()

	startTime := time.Now()
	lastUpdate := time.Now()

	onProgress := func(current, total int64) {
		now := time.Now()
		if now.Sub(lastUpdate) < 200*time.Millisecond && current < total {
			return
		}
		lastUpdate = now

		elapsed := now.Sub(startTime).Seconds()
		if elapsed <= 0 {
			elapsed = 0.1
		}
		speed := float64(current) / elapsed / 1024 / 1024 // MB/s
		percent := float64(current) / float64(total) * 100

		fmt.Fprintf(m.output, "\r%s  └─ 正在分发: %s %.2f%% (%.1f/%.1f MB) %.2f MB/s",
			prefix, fileName, percent, float64(current)/1024/1024, float64(total)/1024/1024, speed)
	}

	if err := m.client.WriteFileWithProgress(remotePkgPath, f, totalSize, onProgress); err != nil {
		fmt.Fprintf(m.output, "\n")
		return fmt.Errorf("upload resource package failed: %v", err)
	}
	fmt.Fprintf(m.output, "\n")

	// 3. 远端清理并解压
	fmt.Fprintf(m.output, "%s  └─ 正在远端解压资源...\n", prefix)
	extractCmd := fmt.Sprintf("cd %s && tar -xzf resources.tar.gz", m.context.RemoteTmpDir)
	if _, err := m.client.RunCommand(extractCmd); err != nil {
		return fmt.Errorf("extract resource package failed: %v", err)
	}

	// 4. 写入标记位
	markCmd := fmt.Sprintf("echo '%s' > %s", localHash, remoteMarkerPath)
	if _, err := m.client.RunCommand(markCmd); err != nil {
		return fmt.Errorf("failed to write success marker: %v", err)
	}

	return nil
}

func (m *Manager) Run(dryRun bool) error {
	if err := m.detectEnv(); err != nil {
		return err
	}

	// 定义日志前缀
	fmt.Printf("----------------------------------------------------------------------------------------\n")
	prefix := fmt.Sprintf("[%s] ", m.nodeCfg.IP)
	role := "worker"
	if m.nodeCfg.IsMaster {
		role = "master"
	}
	fmt.Fprintf(m.output, "%s(%d/%d %s) 检测到 %s %s | KernelVersion: %s | Arch: %s | GPU: %v | NPU: %v\n", prefix,
		m.nodeIndex, m.totalNodes, role, m.context.SystemName, m.context.SystemVersion, m.context.KernelVersion, m.context.Arch, m.context.HasGPU, m.context.HasNPU)

	steps := []runner.Step{
		{
			Name: "分发离线资源",
			Check: func() (bool, error) {
				localHash, err := m.calculateLocalHash()
				if err != nil {
					return false, err
				}
				remoteMarkerPath := path.Join(m.context.RemoteTmpDir, ".extracted_success")
				checkCmd := fmt.Sprintf("cat %s 2>/dev/null || echo 'MISSING'", remoteMarkerPath)
				remoteContent, _ := m.client.RunCommand(checkCmd)
				return strings.TrimSpace(remoteContent) == localHash, nil
			},
			Action: m.distributeResources,
		},
	}

	if m.globalCfg.InstallMode != config.InstallModeAddonsOnly {
		steps = append(steps,
			runner.Step{
				Name:   "禁用 SELinux",
				Check:  m.installer.CheckSELinux,
				Action: m.installer.DisableSELinux,
			},
			runner.Step{
				Name:   "禁用 Firewall",
				Check:  m.installer.CheckFirewall,
				Action: m.installer.DisableFirewall,
			},
			runner.Step{
				Name:   "禁用 Swap分区",
				Check:  m.installer.CheckSwap,
				Action: m.installer.DisableSwap,
			},
			runner.Step{
				Name:   "加载内核模块",
				Check:  m.installer.CheckKernelModules,
				Action: m.installer.LoadKernelModules,
			},
			runner.Step{
				Name:   "配置 Sysctl 内核参数",
				Check:  m.installer.CheckSysctl,
				Action: m.installer.ConfigureSysctl,
			},
			runner.Step{
				Name: "安装常用工具",
				Check: func() (bool, error) {
					return m.installer.CheckCommonTools()
				},
				Action: m.installer.InstallCommonTools,
			},
			runner.Step{
				Name:   "安装 Docker 软件包",
				Check:  m.installer.CheckDockerBinary,
				Action: m.installer.InstallDockerBinary,
			},
			runner.Step{
				Name:   "安装 Containerd 软件包",
				Check:  m.installer.CheckContainerdBinary,
				Action: m.installer.InstallContainerdBinary,
			},
			runner.Step{
				Name:   "安装 Runc 软件包",
				Check:  m.installer.CheckRuncBinary,
				Action: m.installer.InstallRuncBinary,
			},
			runner.Step{
				Name:   "配置cgroup 并启动 Containerd",
				Check:  m.installer.CheckContainerdRunning,
				Action: m.installer.ConfigureAndStartContainerd,
			},
			runner.Step{
				Name:   "配置 Crictl 默认endpoint",
				Check:  m.installer.CheckCrictl,
				Action: m.installer.ConfigureCrictl,
			},
			runner.Step{
				Name:   "安装 Nerdctl",
				Check:  m.installer.CheckNerdctl,
				Action: m.installer.InstallNerdctl,
			},
		)

		if m.isPrimaryExecutionNode() {
			steps = append(steps,
				runner.Step{
					Name: "安装 Helm",
					Check: func() (bool, error) {
						return m.checkHelmInstalled()
					},
					Action: func() error {
						return m.installHelm()
					},
				},
			)
		}

		// ha
		if m.shouldConfigureLoadBalancer() {
			steps = append(steps,
				runner.Step{
					Name: "配置 LB Sysctl 内核参数",
					Check: func() (bool, error) {
						return m.checkLoadBalancerSysctl()
					},
					Action: func() error {
						return m.configureLoadBalancerSysctl()
					},
				},
				runner.Step{
					Name: "安装 HAProxy",
					Check: func() (bool, error) {
						return m.installer.CheckHAProxy()
					},
					Action: func() error {
						return m.installer.InstallHAProxy()
					},
				},
				runner.Step{
					Name: "配置 HAProxy",
					Check: func() (bool, error) {
						return m.checkHAProxyConfig()
					},
					Action: func() error {
						return m.configureHAProxy()
					},
				},
				runner.Step{
					Name: "安装 Keepalived",
					Check: func() (bool, error) {
						return m.installer.CheckKeepalived()
					},
					Action: func() error {
						return m.installer.InstallKeepalived()
					},
				},
				runner.Step{
					Name: "配置 Keepalived",
					Check: func() (bool, error) {
						return m.checkKeepalivedConfig()
					},
					Action: func() error {
						return m.configureKeepalived()
					},
				},
			)
		}

		// registry
		if m.globalCfg.Registry.Endpoint != "" {
			steps = append(steps,
				runner.Step{
					Name:   "配置Containerd 私有镜像仓库",
					Check:  m.installer.CheckConfiguraRegistryContainerd,
					Action: m.installer.ConfiguraRegistryContainerd,
				},
			)
		}

		// 加速卡运行时
		if m.context.HasGPU || m.context.HasNPU {
			steps = append(steps,
				runner.Step{
					Name:   "配置加速卡运行时",
					Check:  m.installer.CheckAcceleratorConfig,
					Action: m.installer.ConfigureAccelerator,
				},
			)
		}

		steps = append(steps,
			runner.Step{
				Name:   "安装 Kubernetes 组件",
				Check:  m.installer.CheckK8sComponents,
				Action: m.installer.InstallK8sComponents,
			})
	}

	// full模式下，需要初始化或加入集群
	if m.globalCfg.InstallMode == config.InstallModeFull {
		steps = append(steps,
			runner.Step{
				Name:   "初始化或加入集群",
				Check:  m.checkClusterStatus,
				Action: m.runKubeadm,
			},
		)
	}

	if m.isPrimaryExecutionNode() && m.globalCfg.InstallMode != config.InstallModePreInit {
		steps = append(steps, m.addonSteps()...)
	}

	// 调用 Runner，传入前缀
	return runner.RunPipeline(steps, prefix, m.output, dryRun)
}

func (m *Manager) shouldConfigureLoadBalancer() bool {
	return m.globalCfg.HA.Enabled && m.nodeCfg.IsMaster
}

func (m *Manager) isPrimaryExecutionNode() bool {
	if !m.nodeCfg.IsMaster {
		return false
	}
	if !m.globalCfg.HA.Enabled {
		return true
	}
	return m.nodeCfg.IsPrimaryMaster
}

func (m *Manager) masterNodeIPs() []string {
	ips := make([]string, 0, len(m.globalCfg.Nodes))
	for _, node := range m.globalCfg.Nodes {
		if node.IsMaster {
			ips = append(ips, node.IP)
		}
	}
	return ips
}

func (m *Manager) virtualIPHost() string {
	vip := strings.TrimSpace(m.globalCfg.HA.VirtualIP)
	if vip == "" {
		return ""
	}
	if strings.Contains(vip, "/") {
		parts := strings.SplitN(vip, "/", 2)
		return parts[0]
	}
	return vip
}

func (m *Manager) checkLoadBalancerSysctl() (bool, error) {
	out, err := m.context.RunCmd("cat /etc/sysctl.d/99-k8s-lb.conf")
	return err == nil && strings.Contains(out, "net.ipv4.ip_nonlocal_bind"), nil
}

func (m *Manager) configureLoadBalancerSysctl() error {
	cmd := `cat <<'EOF' | sudo tee /etc/sysctl.d/99-k8s-lb.conf
net.ipv4.ip_nonlocal_bind = 1
EOF`
	if _, err := m.context.RunCmd(cmd); err != nil {
		return err
	}
	_, err := m.context.RunCmd("sysctl --system")
	return err
}

func (m *Manager) checkHAProxyConfig() (bool, error) {
	out, err := m.context.RunCmd("cat /etc/haproxy/haproxy.cfg")
	if err != nil {
		return false, nil
	}
	return strings.Contains(out, "frontend k8s_api") && strings.Contains(out, "backend k8s_api_backend"), nil
}

func (m *Manager) configureHAProxy() error {
	masterIPs := m.masterNodeIPs()
	if len(masterIPs) == 0 {
		return fmt.Errorf("no master nodes found for haproxy config")
	}
	backendLines := make([]string, 0, len(masterIPs))
	for idx, ip := range masterIPs {
		backendLines = append(backendLines, fmt.Sprintf("  server cp%d %s:6443 check", idx+1, ip))
	}
	config := fmt.Sprintf(`global
  daemon
  maxconn 20000

defaults
  mode tcp
  option tcplog
  timeout connect 5s
  timeout client  1m
  timeout server  1m

# 对外入口：VIP:16443（避免与 apiserver 6443 冲突）
frontend k8s_api
  bind *:16443
  mode tcp
  option tcplog
  default_backend k8s_api_backend

# 后端：三台 apiserver 实际监听的 6443
backend k8s_api_backend
  balance roundrobin
  option tcp-check
  default-server inter 2s fall 3 rise 2
%s
`, strings.Join(backendLines, "\n"))

	cmd := fmt.Sprintf("cp /etc/haproxy/haproxy.cfg /etc/haproxy/haproxy.cfg.bak.$(date +%%F) || true\ncat > /etc/haproxy/haproxy.cfg <<'EOF'\n%s\nEOF", config)
	if _, err := m.context.RunCmd(cmd); err != nil {
		return err
	}
	if _, err := m.context.RunCmd("haproxy -c -f /etc/haproxy/haproxy.cfg"); err != nil {
		return err
	}
	_, err := m.context.RunCmd("systemctl enable --now haproxy")
	return err
}

func (m *Manager) checkKeepalivedConfig() (bool, error) {
	out, err := m.context.RunCmd("cat /etc/keepalived/keepalived.conf")
	if err != nil {
		return false, nil
	}
	vip := m.globalCfg.HA.VirtualIP
	return strings.Contains(out, "vrrp_instance") && strings.Contains(out, vip), nil
}

func (m *Manager) configureKeepalived() error {
	peerIPs := make([]string, 0, len(m.globalCfg.Nodes))
	routerID := ""
	priority := 90
	state := "BACKUP"
	for idx, node := range m.globalCfg.Nodes {
		if !node.IsMaster {
			continue
		}
		if node.IP == m.nodeCfg.IP {
			routerID = fmt.Sprintf("K8S_CP_%d", idx+1)
			if node.IsPrimaryMaster {
				priority = 100
				state = "MASTER"
			} else {
				priority = 90
			}
			continue
		}
		peerIPs = append(peerIPs, node.IP)
	}
	if routerID == "" {
		return fmt.Errorf("failed to determine router_id for keepalived")
	}
	peerLines := make([]string, 0, len(peerIPs))
	for _, ip := range peerIPs {
		peerLines = append(peerLines, fmt.Sprintf("    %s", ip))
	}
	config := fmt.Sprintf(`global_defs {
  router_id %s
}

vrrp_script chk_haproxy {
  script "/etc/keepalived/check_haproxy.sh"
  interval 2
  fall 2
  rise 2
}

vrrp_instance VI_1 {
  state %s
  interface %s
  virtual_router_id 51
  priority %d
  advert_int 1

  authentication {
    auth_type PASS
    auth_pass 123456
  }

  unicast_src_ip %s
  unicast_peer {
%s
  }

  virtual_ipaddress {
    %s
  }

  track_script {
    chk_haproxy
  }
}
`, routerID, state, m.nodeCfg.Interface, priority, m.nodeCfg.IP, strings.Join(peerLines, "\n"), m.globalCfg.HA.VirtualIP)

	if _, err := m.context.RunCmd("sudo mkdir -p /etc/keepalived/"); err != nil {
		return err
	}

	checkScript := `cat <<'EOF' | sudo tee /etc/keepalived/check_haproxy.sh
#!/usr/bin/env bash
set -euo pipefail
systemctl is-active --quiet haproxy
EOF
sudo chmod a+x /etc/keepalived/check_haproxy.sh`

	if _, err := m.context.RunCmd(checkScript); err != nil {
		return err
	}
	cmd := fmt.Sprintf("cat > /etc/keepalived/keepalived.conf <<'EOF'\n%s\nEOF", config)
	if _, err := m.context.RunCmd(cmd); err != nil {
		return err
	}
	_, err := m.context.RunCmd("systemctl enable --now keepalived")
	return err
}

func (m *Manager) addonSteps() []runner.Step {
	mode := m.globalCfg.InstallMode
	steps := []runner.Step{}

	// 1. Kube-OVN CNI (Full or AddonsOnly)
	if m.globalCfg.Addons.KubeOvn.Enabled {
		steps = append(steps, runner.Step{
			Name: "部署 Kube-OVN CNI",
			Check: func() (bool, error) {
				out, err := m.context.RunCmd("test -e /etc/cni/net.d/01-kube-ovn.conflist && echo EXISTS || echo MISSING")
				if err != nil {
					return true, err
				}
				if strings.TrimSpace(out) == "EXISTS" {
					return true, nil
				}
				return false, nil
			},
			Action: m.deployKubeOvn,
		})
	}

	// 2. Multus CNI (Full or AddonsOnly)
	if m.globalCfg.Addons.MultusCNI.Enabled {
		steps = append(steps, runner.Step{
			Name: "部署 Multus CNI",
			Check: func() (bool, error) {
				out, err := m.context.RunCmd("test -e /etc/cni/net.d/00-multus.conf && echo EXISTS || echo MISSING")
				if err != nil {
					return true, err
				}
				if strings.TrimSpace(out) == "EXISTS" {
					return true, nil
				}
				return false, nil
			},
			Action: m.deployMultusCNI,
		})
	}

	// 3. kube-prometheus-stack (AddonsOnly only)
	if mode == config.InstallModeAddonsOnly && m.globalCfg.Addons.KubePrometheus.Enabled {
		steps = append(steps, runner.Step{
			Name: "部署 kube-prometheus-stack",
			Check: func() (bool, error) {
				out, err := m.context.RunCmd("helm -n monitoring list -q | grep -w '^kube-prometheus-stack$' || true")
				if err != nil {
					return false, err
				}
				return strings.TrimSpace(out) == "kube-prometheus-stack", nil
			},
			Action: m.deployKubePrometheusStack,
		})
	}

	// 4. HAMI (AddonsOnly only)
	if mode == config.InstallModeAddonsOnly && m.globalCfg.Addons.Hami.Enabled {
		steps = append(steps, runner.Step{
			Name: "部署 HAMI",
			Check: func() (bool, error) {
				out, err := m.context.RunCmd("helm -n kube-system list -q | grep -w '^hami$' || true")
				if err != nil {
					return false, err
				}
				return strings.TrimSpace(out) == "hami", nil
			},
			Action: m.deployHami,
		})

		steps = append(steps, runner.Step{
			Name: "部署 HAMI-WebUI",
			Check: func() (bool, error) {
				out, err := m.context.RunCmd("helm -n kube-system list -q | grep -w '^hami-webui$' || true")
				if err != nil {
					return false, err
				}
				return strings.TrimSpace(out) == "hami-webui", nil
			},
			Action: m.deployHamiWebUI,
		})
	}

	// 5. ascend-device-plugin (AddonsOnly)
	// 仅在 addons-only 模式、已成功部署 HAMi 且集群中存在 Ascend 节点时才会触发。
	if mode == config.InstallModeAddonsOnly && m.globalCfg.Addons.Hami.Enabled {
		steps = append(steps, runner.Step{
			Name: "部署 ascend-device-plugin",
			Check: func() (bool, error) {
				hamiOut, _ := m.context.RunCmd("helm -n kube-system list -q | grep -w '^hami$' || true")
				if strings.TrimSpace(hamiOut) != "hami" {
					return true, nil // Skip if hami not found
				}

				npuOut, _ := m.context.RunCmd("kubectl get node -l ascend=on -o name")
				if strings.TrimSpace(npuOut) == "" {
					return true, nil // Skip if no ascend nodes
				}

				out, err := m.context.RunCmd("kubectl get ds -n kube-system hami-ascend-device-plugin --ignore-not-found -o name")
				if err != nil {
					return false, err
				}
				return strings.TrimSpace(out) != "", nil
			},
			Action: m.deployAscendDevicePlugin,
		})
	}

	return steps
}

func (m *Manager) registryHost() (string, bool) {
	if strings.TrimSpace(m.globalCfg.Registry.Endpoint) == "" {
		return "", false
	}
	return fmt.Sprintf("%s:%d", m.globalCfg.Registry.Endpoint, m.globalCfg.Registry.Port), true
}

func (m *Manager) ensureAdminConf() error {
	_, err := m.context.RunCmd("test -f /etc/kubernetes/admin.conf")
	if err != nil {
		return fmt.Errorf("admin.conf not found on master node")
	}
	return nil
}

func (m *Manager) deployHelmAddon(name, groupKey, relativePath, chartName, namespace string) error {
	if err := m.ensureAdminConf(); err != nil {
		return err
	}
	chartPath := path.Join(m.context.RemoteTmpDir, "helm-resource", relativePath, chartName)
	valuesPath := path.Join(m.context.RemoteTmpDir, "helm-resource", relativePath, "values.yaml")
	if err := m.rewriteHelmValuesFile(groupKey, valuesPath); err != nil {
		return err
	}
	cmd := fmt.Sprintf("helm install %s %s -n %s -f %s --create-namespace", name, chartPath, namespace, valuesPath)
	_, err := m.context.RunCmd(cmd)
	return err
}

func (m *Manager) deployKubeOvn() error {
	_, err := m.context.RunCmd("kubectl label node -l beta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite > /dev/null 2>&1 && kubectl label node -l node-role.kubernetes.io/control-plane kube-ovn/role=master --overwrite > /dev/null 2>&1")
	if err != nil {
		return err
	}
	return m.deployHelmAddon("kube-ovn", "kube-ovn-images", path.Join("cni", "kube-ovn"), config.DefaultKubeOvnChart, "kube-system")
}

func (m *Manager) deployMultusCNI() error {
	if err := m.ensureAdminConf(); err != nil {
		return err
	}
	manifestPath := path.Join(m.context.RemoteTmpDir, "cni", "multus-cni", "multus-daemonset-thick.yml")
	if registryHost, ok := m.registryHost(); ok {
		image := registryHost + "/" + config.DefaultMultusImage
		cmd := fmt.Sprintf("sed -i 's|ghcr.io/k8snetworkplumbingwg/multus-cni:snapshot-thick|%s|g' %s", image, manifestPath)
		if _, err := m.context.RunCmd(cmd); err != nil {
			return err
		}
	}
	cmd := fmt.Sprintf("kubectl apply -f %s", manifestPath)
	_, err := m.context.RunCmd(cmd)
	return err
}

func (m *Manager) deployKubePrometheusStack() error {
	return m.deployHelmAddon("kube-prometheus-stack", "kube-prometheus-stack-images", "kube-prometheus-stack", config.DefaultKubePrometheusStackChart, "monitoring")
}

func (m *Manager) deployHami() error {
	if err := m.ensureAdminConf(); err != nil {
		return err
	}

	// 1. 遍历所有节点并打标
	hasAscend, err := m.labelAcceleratorNodes()
	if err != nil {
		return fmt.Errorf("failed to label accelerator nodes: %v", err)
	}

	// 2. 修改 values.yaml
	valuesPath := path.Join(m.context.RemoteTmpDir, "helm-resource", "hami", "hami", "values.yaml")
	if hasAscend {
		// 使用更精确的 sed 命令：匹配以 'ascend:' 开头的行，并在该行到后续 'enabled:' 出现的范围内，将 'enabled: false' 替换为 'enabled: true'
		cmd := fmt.Sprintf("sed -i '/ascend:/,/enabled:/ s/enabled: false/enabled: true/' %s", valuesPath)
		if _, err := m.context.RunCmd(cmd); err != nil {
			return fmt.Errorf("failed to enable ascend in hami values.yaml: %v", err)
		}
	}

	return m.deployHelmAddon("hami", "hami-images", path.Join("hami", "hami"), config.DefaultHamiChart, "kube-system")
}

func (m *Manager) labelAcceleratorNodes() (bool, error) {
	// 获取集群节点 IP 到 名称的映射
	nodeMapCmd := "kubectl get nodes -o custom-columns='NAME:.metadata.name,IP:.status.addresses[?(@.type==\"InternalIP\")].address' --no-headers"
	out, err := m.context.RunCmd(nodeMapCmd)
	if err != nil {
		return false, err
	}

	ipToName := make(map[string]string)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			ipToName[parts[1]] = parts[0]
		}
	}

	hasAscendTotal := false
	for _, node := range m.globalCfg.Nodes {
		//nodeName := "bms-d838"
		nodeName, ok := ipToName[node.IP]
		if !ok {
			continue
		}

		// 探测节点加速卡类型
		// 我们通过 SSH 连接到每个节点进行探测
		gpu, npu, err := m.probeNodeAccelerators(node)
		if err != nil {
			fmt.Fprintf(m.output, "  └─ [Warning] Failed to probe accelerators on %s: %v\n", node.IP, err)
			continue
		}

		if gpu {
			m.context.RunCmd(fmt.Sprintf("kubectl label node %s gpu=on --overwrite", nodeName))
		}
		if npu {
			m.context.RunCmd(fmt.Sprintf("kubectl label node %s ascend=on --overwrite", nodeName))
			hasAscendTotal = true
		}
	}
	return hasAscendTotal, nil
}

func (m *Manager) probeNodeAccelerators(node config.NodeConfig) (bool, bool, error) {
	// 如果是当前节点，直接用 context
	if node.IP == m.nodeCfg.IP {
		return m.context.HasGPU, m.context.HasNPU, nil
	}

	// 否则需要建立临时 SSH 连接
	port := node.SSHPort
	if port == 0 {
		port = m.globalCfg.SSHPort
	}
	client, err := ssh.NewClient(node.IP, port, m.globalCfg.User, node.Password, time.Duration(m.globalCfg.CommandTimeoutSeconds)*time.Second)
	if err != nil {
		return false, false, err
	}
	defer client.Close()

	probeCmd := `
gpu="false"; if lspci | grep -i nvidia >/dev/null 2>&1; then gpu="true"; fi
npu="false"; if lspci | grep -i "Huawei" >/dev/null 2>&1; then npu="true"; fi
echo "${gpu}|${npu}"
`
	out, err := client.RunCommand(strings.TrimSpace(probeCmd))
	if err != nil {
		return false, false, err
	}
	parts := strings.Split(strings.TrimSpace(out), "|")
	if len(parts) != 2 {
		return false, false, fmt.Errorf("unexpected probe output: %s", out)
	}
	return parts[0] == "true", parts[1] == "true", nil
}

func (m *Manager) deployAscendDevicePlugin() error {
	if err := m.ensureAdminConf(); err != nil {
		return err
	}
	manifestPath := path.Join(m.context.RemoteTmpDir, "helm-resource", "hami", "ascend-device-plugin", "ascend-device-plugin.yaml")

	// 处理私有仓库
	if registryHost, ok := m.registryHost(); ok {
		cmd := fmt.Sprintf("sed -i 's|docker.io|%s|g' %s", registryHost, manifestPath)
		if _, err := m.context.RunCmd(cmd); err != nil {
			return err
		}
	}

	cmd := fmt.Sprintf("kubectl apply -f %s", manifestPath)
	_, err := m.context.RunCmd(cmd)
	return err
}

func (m *Manager) deployHamiWebUI() error {
	return m.deployHelmAddon("hami-webui", "hami-webui-images", path.Join("hami", "hami-webui"), config.DefaultHamiWebUIChart, "kube-system")
}

func (m *Manager) checkHelmInstalled() (bool, error) {
	out, err := m.context.RunCmd("helm version --short")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(out) != "", nil
}

func (m *Manager) installHelm() error {
	tarballDir := path.Join(m.context.RemoteTmpDir, "helm", m.context.Arch)
	cmd := fmt.Sprintf("cd %s && tar -zxvf helm-v*-linux-%s.tar.gz >/dev/null && mv linux-%s/helm /usr/local/bin/helm && rm -rf linux-%s", tarballDir, m.context.Arch, m.context.Arch, m.context.Arch)
	_, err := m.context.RunCmd(cmd)
	return err
}

func (m *Manager) rewriteHelmValuesFile(groupKey, valuesPath string) error {
	registryHost, ok := m.registryHost()
	if !ok {
		return nil
	}
	groups, err := config.ImagesByGroup()
	if err != nil {
		return err
	}
	images := groups[groupKey]
	if len(images) == 0 {
		return nil
	}

	replacements := make(map[string]string)
	for _, image := range images {
		srcRepo, _ := splitImage(image)
		dstRepo, _ := splitImage(replaceImageRegistry(image, registryHost))
		replacements[srcRepo] = dstRepo
		srcRegistry := strings.SplitN(srcRepo, "/", 2)[0]
		if _, exists := replacements[srcRegistry]; !exists {
			replacements[srcRegistry] = registryHost
		}
	}

	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return len(b) - len(a)
	})

	for _, oldValue := range keys {
		newValue := replacements[oldValue]
		cmd := fmt.Sprintf("sed -i 's|%s|%s|g' %s", regexp.QuoteMeta(oldValue), newValue, valuesPath)
		if _, err := m.context.RunCmd(cmd); err != nil {
			return err
		}
	}
	return nil
}

func splitImage(image string) (string, string) {
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		return image[:lastColon], image[lastColon+1:]
	}
	return image, "latest"
}

func replaceImageRegistry(image, registry string) string {
	parts := strings.SplitN(image, "/", 2)
	if len(parts) < 2 {
		return registry + "/" + image
	}
	return registry + "/" + parts[1]
}

func (m *Manager) runLocalCmd(cmd string) (string, error) {
	output, err := exec.Command("bash", "-lc", cmd).CombinedOutput()
	outText := strings.TrimSpace(string(output))
	if err != nil {
		return outText, fmt.Errorf("local command failed: %w: %s", err, outText)
	}
	return outText, nil
}

func (m *Manager) checkClusterStatus() (bool, error) {
	out, err := m.client.RunCommand("ls /etc/kubernetes/admin.conf")
	// 注意：对于 Worker 节点，可能没有 admin.conf，可以检查 kubelet.conf
	if !m.nodeCfg.IsMaster {
		out, err = m.client.RunCommand("ls /etc/kubernetes/kubelet.conf")
	} else {
		if err == nil && out != "" {
			if !m.isPrimaryExecutionNode() {
				return true, nil
			}
			err = m.generateClusterJoinCommands()
			if err != nil {
				return false, err
			}
		}
	}

	return err == nil && out != "", nil
}

func (m *Manager) runKubeadm() error {
	if m.nodeCfg.IsMaster {
		if !m.isPrimaryExecutionNode() {
			// 次master节点加入集群
			if strings.TrimSpace(m.globalCfg.MasterJoinCommand) == "" {
				return fmt.Errorf("master join command is required for HA mode")
			}
			_, err := m.client.RunCommand(m.globalCfg.MasterJoinCommand)
			return err
		}
		// 主master节点初始化集群
		repo := "registry.aliyuncs.com/google_containers"
		if m.globalCfg.Registry.Endpoint != "" {
			repo = fmt.Sprintf("%s:%d/google_containers", m.globalCfg.Registry.Endpoint, m.globalCfg.Registry.Port)
		}

		controlPlaneEndpoint := ""
		if m.globalCfg.HA.Enabled {
			controlPlaneEndpoint = fmt.Sprintf(` \
--control-plane-endpoint "%s:16443" \
--upload-certs`, m.virtualIPHost())
		}
		cmd := fmt.Sprintf(`kubeadm init --v 0 \
--kubernetes-version=v%s \
--image-repository=%s%s`, m.globalCfg.Versions.K8s, repo, controlPlaneEndpoint)

		_, err := m.client.RunCommand(cmd)
		if err != nil {
			return err
		}

		m.client.RunCommand("mkdir -p $HOME/.kube && cp -f /etc/kubernetes/admin.conf $HOME/.kube/config && chown $(id -u):$(id -g) $HOME/.kube/config")

		err = m.generateClusterJoinCommands()
		if err != nil {
			return err
		}

		return nil
	} else {
		// Worker 节点加入集群
		joinCmd := m.globalCfg.JoinCommand
		if joinCmd != "" {
			_, err := m.client.RunCommand(joinCmd)
			return err
		}
	}
	return nil
}

func (m *Manager) generateClusterJoinCommands() error {
	out, err := m.client.RunCommand("kubeadm token create --print-join-command")
	if err != nil {
		return fmt.Errorf("kubeadm token create --print-join-command failed, %s", out)
	}
	m.globalCfg.JoinCommand = strings.TrimSpace(out)

	if !m.globalCfg.HA.Enabled {
		return nil
	}
	certOut, err := m.client.RunCommand("kubeadm init phase upload-certs --upload-certs")
	if err != nil {
		return fmt.Errorf("kubeadm init phase upload-certs failed, %s", certOut)
	}
	certKey := extractCertificateKey(certOut)
	if certKey == "" {
		return fmt.Errorf("failed to parse certificate key from output: %s", certOut)
	}
	m.globalCfg.MasterJoinCommand = fmt.Sprintf("%s --control-plane --certificate-key %s", strings.TrimSpace(out), certKey)
	return nil
}

func extractCertificateKey(output string) string {
	re := regexp.MustCompile(`[a-f0-9]{32,64}`)
	matches := re.FindAllString(strings.ToLower(output), -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}
