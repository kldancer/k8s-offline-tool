package install

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"k8s-offline-tool/pkg/assets"
	"k8s-offline-tool/pkg/config"
	"k8s-offline-tool/pkg/install/strategy"
	"k8s-offline-tool/pkg/runner"
	"k8s-offline-tool/pkg/ssh"
)

// Manager 现在对应一个节点的安装任务
type Manager struct {
	globalCfg *config.Config
	nodeCfg   *config.NodeConfig
	client    *ssh.Client
	installer strategy.NodeInstaller
	context   *strategy.Context
	output    io.Writer
}

// NewManager 创建针对特定节点的管理器
func NewManager(globalCfg *config.Config, nodeCfg *config.NodeConfig, output io.Writer) (*Manager, error) {
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
		globalCfg: globalCfg,
		nodeCfg:   nodeCfg,
		client:    client,
		output:    output,
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

	// 获取系统名称
	systemName := "unknown"
	if verOut, err := m.client.RunCommand("grep '^NAME=' /etc/os-release | cut -d= -f2 | sed 's/\"//g'"); err == nil {
		ver := strings.TrimSpace(verOut)
		if ver != "" {
			systemName = ver
		}
	}

	// 获取系统版本
	systemVersion := "unknown"
	if verOut, err := m.client.RunCommand("grep '^VERSION_ID=' /etc/os-release | cut -d= -f2 | sed 's/\"//g'"); err == nil {
		ver := strings.TrimSpace(verOut)
		if ver != "" {
			systemVersion = ver
		}
	}

	// 获取内核版本
	kernelVersion, err := m.client.RunCommand("uname -r")
	if err != nil {
		return fmt.Errorf("failed to detect kernel version: %v", err)
	}
	kernelVersion = strings.TrimSpace(kernelVersion)

	hasGPU := false
	if gpuOut, _ := m.client.RunCommand("lspci | grep -i nvidia"); gpuOut != "" {
		hasGPU = true
	}

	m.context = &strategy.Context{
		Cfg:           m.globalCfg, // 注意：Context 中传递 Global 配置用于获取 Registry/Versions
		Arch:          arch,
		SystemName:    systemName,
		SystemVersion: systemVersion,
		KernelVersion: kernelVersion,
		HasGPU:        hasGPU,
		RemoteTmpDir:  config.RemoteTmpDir,
		RunCmd:        m.client.RunCommand,
	}

	osInfo := strings.ToLower(systemName)
	if strings.Contains(osInfo, "fedora") || strings.Contains(osInfo, "centos") {
		m.installer = &strategy.FedoraInstaller{Ctx: m.context}
	} else if strings.Contains(osInfo, "ubuntu") || strings.Contains(osInfo, "debian") {
		m.installer = &strategy.UbuntuInstaller{Ctx: m.context}
	} else {
		return fmt.Errorf("unsupported OS: %s", osInfo)
	}
	return nil
}

func (m *Manager) distributeResources() error {
	fsys, err := assets.GetFileSystem()
	if err != nil {
		return err
	}

	targetArch := m.context.Arch
	isFedora := strings.Contains(m.installer.Name(), "Fedora")

	shouldSkip := func(rawPath string, isDir bool) bool {
		p := filepath.ToSlash(rawPath)
		if targetArch == "amd64" && strings.Contains(p, "arm64") {
			return true
		}
		if targetArch == "arm64" && strings.Contains(p, "amd64") {
			return true
		}
		if isFedora && strings.Contains(p, "/apt") {
			return true
		}
		if !isFedora && strings.Contains(p, "/rpm") {
			return true
		}
		if !m.context.HasGPU && strings.Contains(p, "nvidia-container-toolkit") {
			return true
		}
		if !m.shouldIncludeAddonPath(p, isDir) {
			return true
		}
		return false
	}

	// 1. 统计总数
	totalFiles := 0
	err = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if shouldSkip(path, d.IsDir()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			totalFiles++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to count files: %v", err)
	}

	// 2. 执行上传
	current := 0
	prefix := fmt.Sprintf("[%s] ", m.nodeCfg.IP)

	return fs.WalkDir(fsys, ".", func(relPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		slashPath := filepath.ToSlash(relPath)
		if shouldSkip(relPath, d.IsDir()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}

		// --- 打印“开始上传” ---
		current++
		fileName := path.Base(slashPath)
		if len(fileName) > 25 {
			fileName = fileName[:22] + "..."
		}

		// 计算进度条
		percent := float64(current) / float64(totalFiles) * 100
		const barWidth = 20
		filled := int(float64(barWidth) * float64(current) / float64(totalFiles))
		if filled > barWidth {
			filled = barWidth
		}
		bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
		if filled > 0 && filled < barWidth {
			bar = strings.Repeat("=", filled-1) + ">" + strings.Repeat(" ", barWidth-filled)
		}

		// 关键点：在 WriteFile 之前打印，让用户知道正在传这个文件
		fmt.Fprintf(m.output, "\r%s  └─ Syncing: [%s] %3.0f%% (%d/%d) %-25s", prefix, bar, percent, current, totalFiles, fileName)

		// --- 开始流式传输 ---
		// 使用 Open 打开流，而不是 ReadFile 读入内存
		f, err := fsys.Open(relPath)
		if err != nil {
			return err
		}
		defer f.Close()

		remotePath := path.Join(m.context.RemoteTmpDir, slashPath)

		// 调用新版的 WriteFile (传入 io.Reader)
		if err := m.client.WriteFile(remotePath, f); err != nil {
			return fmt.Errorf("upload %s failed: %v", fileName, err)
		}

		if current == totalFiles {
			fmt.Fprint(m.output, "\n")
		}

		return nil
	})
}

func (m *Manager) Run(dryRun bool) error {
	if err := m.detectEnv(); err != nil {
		return err
	}

	// 定义日志前缀
	fmt.Printf("----------------------------------------------------------------------------------------\n")
	prefix := fmt.Sprintf("[%s] ", m.nodeCfg.IP)
	fmt.Fprintf(m.output, "%s检测到 %s %s | KernelVersion: %s | Arch: %s | GPU: %v\n", prefix,
		m.context.SystemName, m.context.SystemVersion, m.context.KernelVersion, m.context.Arch, m.context.HasGPU)

	steps := []runner.Step{
		{
			Name: "分发离线资源",
			Check: func() (bool, error) {
				out, err := m.client.RunCommand(fmt.Sprintf("test -d %q && echo EXISTS || echo MISSING", m.context.RemoteTmpDir))
				if err != nil {
					return false, nil
				}
				return strings.TrimSpace(out) == "EXISTS", nil // 存在
			},
			Action: m.distributeResources,
		},
	}

	if m.globalCfg.InstallMode == config.InstallModeFull {
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
					if !m.nodeCfg.InstallTools {
						return true, nil
					}
					return m.installer.CheckCommonTools()
				},
				Action: m.installer.InstallCommonTools,
			},
			runner.Step{
				Name:   "安装 Containerd 二进制文件",
				Check:  m.installer.CheckContainerdBinaries,
				Action: m.installer.InstallContainerdBinaries,
			},
			runner.Step{
				Name:   "安装 Runc",
				Check:  m.installer.CheckRunc,
				Action: m.installer.InstallRunc,
			},
			runner.Step{
				Name:   "配置 Containerd 服务",
				Check:  m.installer.CheckContainerdService,
				Action: m.installer.ConfigureContainerdService,
			},
			runner.Step{
				Name:   "配置cgroup、私有镜像仓库并启动 Containerd",
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
			runner.Step{
				Name:   "配置 GPU 运行时",
				Check:  m.installer.CheckGPUConfig,
				Action: m.installer.ConfigureGPU,
			},
			runner.Step{
				Name:   "安装 Kubernetes 组件",
				Check:  m.installer.CheckK8sComponents,
				Action: m.installer.InstallK8sComponents,
			},
			runner.Step{
				Name:   "初始化或加入集群",
				Check:  m.checkClusterStatus,
				Action: m.runKubeadm,
			},
		)
	}

	steps = append(steps, m.addonSteps()...)

	// 调用 Runner，传入前缀
	return runner.RunPipeline(steps, prefix, m.output, dryRun)
}

func (m *Manager) addonSteps() []runner.Step {
	return []runner.Step{
		{
			Name: "同步镜像到私有仓库",
			Check: func() (bool, error) {
				if !m.nodeCfg.IsMaster || m.globalCfg.Registry.Endpoint == "" {
					return true, nil
				}
				return false, nil
			},
			Action: m.syncImagesToRegistry,
		},
		{
			Name: "部署 Kube-OVN CNI",
			Check: func() (bool, error) {
				if !m.nodeCfg.IsMaster || !m.globalCfg.Addons.KubeOvn.Enabled {
					return true, nil
				}
				return false, nil
			},
			Action: m.deployKubeOvn,
		},
		{
			Name: "部署 Multus CNI",
			Check: func() (bool, error) {
				if !m.nodeCfg.IsMaster || !m.globalCfg.Addons.MultusCNI.Enabled {
					return true, nil
				}
				return false, nil
			},
			Action: m.deployMultusCNI,
		},
		{
			Name: "部署 Local Path Storage",
			Check: func() (bool, error) {
				if !m.nodeCfg.IsMaster || !m.globalCfg.Addons.LocalPathStorage.Enabled {
					return true, nil
				}
				return false, nil
			},
			Action: m.deployLocalPathStorage,
		},
	}
}

func (m *Manager) registryHost() (string, bool) {
	if strings.TrimSpace(m.globalCfg.Registry.Endpoint) == "" {
		return "", false
	}
	return fmt.Sprintf("%s:%d", m.globalCfg.Registry.Endpoint, m.globalCfg.Registry.Port), true
}

func (m *Manager) imageClient() (string, error) {
	if _, err := m.context.RunCmd("nerdctl --version"); err == nil {
		return "nerdctl", nil
	}
	if _, err := m.context.RunCmd("ctr version"); err == nil {
		return "ctr", nil
	}
	return "", fmt.Errorf("nerdctl or ctr is required to sync images")
}

func (m *Manager) syncImagesToRegistry() error {
	registryHost, ok := m.registryHost()
	if !ok {
		return nil
	}
	client, err := m.imageClient()
	if err != nil {
		return err
	}
	for _, image := range config.RequiredImages {
		target := replaceImageRegistry(image, registryHost)
		repo, tag := splitImage(target)
		repoPath := strings.TrimPrefix(repo, registryHost+"/")
		checkCmd := fmt.Sprintf("curl -sf -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' http://%s/v2/%s/manifests/%s > /dev/null", registryHost, repoPath, tag)
		if _, err := m.context.RunCmd(checkCmd); err == nil {
			continue
		}
		switch client {
		case "nerdctl":
			if _, err := m.context.RunCmd(fmt.Sprintf("nerdctl pull %s", image)); err != nil {
				return err
			}
			if _, err := m.context.RunCmd(fmt.Sprintf("nerdctl tag %s %s", image, target)); err != nil {
				return err
			}
			// 不用添加 --insecure-registry，因为已经配置了http
			if _, err := m.context.RunCmd(fmt.Sprintf("nerdctl  push %s", target)); err != nil {
				return err
			}
		case "ctr":
			if _, err := m.context.RunCmd(fmt.Sprintf("ctr images pull %s", image)); err != nil {
				return err
			}
			if _, err := m.context.RunCmd(fmt.Sprintf("ctr images tag %s %s", image, target)); err != nil {
				return err
			}
			if _, err := m.context.RunCmd(fmt.Sprintf("ctr images push --plain-http %s", target)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) ensureAdminConf() error {
	_, err := m.context.RunCmd("test -f /etc/kubernetes/admin.conf")
	if err != nil {
		return fmt.Errorf("admin.conf not found on master node")
	}
	return nil
}

func (m *Manager) deployKubeOvn() error {
	if err := m.ensureAdminConf(); err != nil {
		return err
	}
	versionDir := versionToDir(m.globalCfg.Addons.KubeOvn.Version)
	installDir := path.Join(m.context.RemoteTmpDir, "cni", "kube-ovn", versionDir)
	installScript := path.Join(installDir, "install.sh")
	if registryHost, ok := m.registryHost(); ok {
		registry := registryHost + "/kubeovn"
		cmd := fmt.Sprintf("sed -i 's|^REGISTRY=.*|REGISTRY=\"%s\"|' %s", registry, installScript)
		if _, err := m.context.RunCmd(cmd); err != nil {
			return err
		}
	}
	cmd := fmt.Sprintf("bash %s ", installScript)
	_, err := m.context.RunCmd(cmd)
	return err
}

func (m *Manager) deployMultusCNI() error {
	if err := m.ensureAdminConf(); err != nil {
		return err
	}
	manifestPath := path.Join(m.context.RemoteTmpDir, "cni", "multus-cni ", "multus-daemonset-thick.yml")
	if registryHost, ok := m.registryHost(); ok {
		image := registryHost + "/k8snetworkplumbingwg/multus-cni:snapshot-thick"
		cmd := fmt.Sprintf("sed -i 's|ghcr.io/k8snetworkplumbingwg/multus-cni:snapshot-thick|%s|g' %s", image, manifestPath)
		if _, err := m.context.RunCmd(cmd); err != nil {
			return err
		}
	}
	cmd := fmt.Sprintf("kubectl apply -f %s", manifestPath)
	_, err := m.context.RunCmd(cmd)
	return err
}

func (m *Manager) deployLocalPathStorage() error {
	if err := m.ensureAdminConf(); err != nil {
		return err
	}
	versionDir := versionToDir(m.globalCfg.Addons.LocalPathStorage.Version)
	manifestPath := path.Join(m.context.RemoteTmpDir, "local-path-provisioner", versionDir, "local-path-storage.yaml")
	if registryHost, ok := m.registryHost(); ok {
		image := registryHost + "/rancher/local-path-provisioner:v0.0.34"
		cmd := fmt.Sprintf("sed -i 's|rancher/local-path-provisioner:v0.0.34|%s|g' %s", image, manifestPath)
		if _, err := m.context.RunCmd(cmd); err != nil {
			return err
		}
	}
	cmd := fmt.Sprintf("kubectl apply -f %s", manifestPath)
	_, err := m.context.RunCmd(cmd)
	return err
}

func (m *Manager) shouldIncludeAddonPath(p string, isDir bool) bool {
	if m.globalCfg.InstallMode == config.InstallModeAddonsOnly {
		if !m.isAddonPath(p) {
			return false
		}
		return m.isAddonPathEnabled(p, isDir)
	}
	if m.isAddonPath(p) {
		return m.isAddonPathEnabled(p, isDir)
	}
	return true
}

func (m *Manager) isAddonPath(p string) bool {
	return p == "cni" ||
		strings.HasPrefix(p, "cni/kube-ovn") ||
		strings.HasPrefix(p, "cni/multus-cni ") ||
		strings.HasPrefix(p, "local-path-provisioner")
}

func (m *Manager) isAddonPathEnabled(p string, isDir bool) bool {
	if p == "cni" {
		return m.globalCfg.Addons.KubeOvn.Enabled || m.globalCfg.Addons.MultusCNI.Enabled
	}
	if strings.HasPrefix(p, "cni/kube-ovn") {
		if !m.globalCfg.Addons.KubeOvn.Enabled {
			return false
		}
		versionDir := path.Join("cni", "kube-ovn", versionToDir(m.globalCfg.Addons.KubeOvn.Version))
		return p == "cni/kube-ovn" || p == versionDir || strings.HasPrefix(p, versionDir+"/")
	}
	if strings.HasPrefix(p, "cni/multus-cni ") {
		return m.globalCfg.Addons.MultusCNI.Enabled
	}
	if strings.HasPrefix(p, "local-path-provisioner") {
		if !m.globalCfg.Addons.LocalPathStorage.Enabled {
			return false
		}
		versionDir := path.Join("local-path-provisioner", versionToDir(m.globalCfg.Addons.LocalPathStorage.Version))
		return p == "local-path-provisioner" || p == versionDir || strings.HasPrefix(p, versionDir+"/")
	}
	return false
}

func versionToDir(version string) string {
	return strings.ReplaceAll(version, ".", "-")
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

func (m *Manager) checkClusterStatus() (bool, error) {
	out, err := m.client.RunCommand("ls /etc/kubernetes/admin.conf")
	// 注意：对于 Worker 节点，可能没有 admin.conf，可以检查 kubelet.conf
	if !m.nodeCfg.IsMaster {
		out, err = m.client.RunCommand("ls /etc/kubernetes/kubelet.conf")
	}
	if m.nodeCfg.IsMaster && err == nil && out != "" {
		err = m.generateClusterJoinCommand()
		if err != nil {
			return false, err
		}
	}

	return err == nil && out != "", nil
}

func (m *Manager) runKubeadm() error {
	if m.nodeCfg.IsMaster {
		repo := "registry.aliyuncs.com/google_containers"
		if m.globalCfg.Registry.Endpoint != "" {
			repo = fmt.Sprintf("%s:%d/google_containers", m.globalCfg.Registry.Endpoint, m.globalCfg.Registry.Port)
		}

		cmd := fmt.Sprintf(`kubeadm init --v 0 \
--kubernetes-version=v%s \
--image-repository=%s`, m.globalCfg.Versions.K8s, repo)

		_, err := m.client.RunCommand(cmd)
		if err != nil {
			return err
		}
		//fmt.Fprintf(m.output, "[%s] Init Result:\n%s\n", m.nodeCfg.IP, out)

		m.client.RunCommand("mkdir -p $HOME/.kube && cp -i /etc/kubernetes/admin.conf $HOME/.kube/config && chown $(id -u):$(id -g) $HOME/.kube/config")

		err = m.generateClusterJoinCommand()
		if err != nil {
			return err
		}

		return nil
	} else {
		// Worker 节点
		joinCmd := m.globalCfg.JoinCommand
		if joinCmd != "" {
			_, err := m.client.RunCommand(joinCmd)
			return err
		}
	}
	return nil
}

func (m *Manager) generateClusterJoinCommand() error {
	out, err := m.client.RunCommand("kubeadm token create --print-join-command")
	if err != nil {
		return fmt.Errorf("kubeadm token create --print-join-command failed, %s", out)
	}
	fmt.Fprintf(m.output, "\n[%s] Token Create Result: %s ", m.nodeCfg.IP, out)
	m.globalCfg.JoinCommand = out
	return nil
}
