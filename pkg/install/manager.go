package install

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

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
}

// NewManager 创建针对特定节点的管理器
func NewManager(globalCfg *config.Config, nodeCfg *config.NodeConfig) (*Manager, error) {
	user := globalCfg.User
	port := nodeCfg.SSHPort
	if port == 0 {
		port = globalCfg.SSHPort
	}

	client, err := ssh.NewClient(nodeCfg.IP, port, user, nodeCfg.Password)
	if err != nil {
		return nil, fmt.Errorf("ssh connection to %s failed: %v", nodeCfg.IP, err)
	}

	return &Manager{
		globalCfg: globalCfg,
		nodeCfg:   nodeCfg,
		client:    client,
	}, nil
}

func (m *Manager) Close() {
	if m.client != nil {
		m.client.Close()
	}
}

func (m *Manager) detectEnv() error {
	out, err := m.client.RunCommand("cat /etc/os-release")
	if err != nil {
		return fmt.Errorf("failed to read /etc/os-release: %v", err)
	}

	arch, err := m.client.DetectArch()
	if err != nil {
		return fmt.Errorf("failed to detect arch: %v", err)
	}

	hasGPU := false
	if gpuOut, _ := m.client.RunCommand("lspci | grep -i nvidia"); gpuOut != "" {
		hasGPU = true
	}

	m.context = &strategy.Context{
		Cfg:          m.globalCfg, // 注意：Context 中传递 Global 配置用于获取 Registry/Versions
		Arch:         arch,
		HasGPU:       hasGPU,
		RemoteTmpDir: config.RemoteTmpDir,
		RunCmd:       m.client.RunCommand,
	}

	osInfo := strings.ToLower(out)
	if strings.Contains(osInfo, "fedora") || strings.Contains(osInfo, "centos") {
		m.installer = &strategy.FedoraInstaller{Ctx: m.context}
	} else if strings.Contains(osInfo, "ubuntu") || strings.Contains(osInfo, "debian") {
		m.installer = &strategy.UbuntuInstaller{Ctx: m.context}
	} else {
		return fmt.Errorf("unsupported OS: %s", out)
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
		fmt.Printf("\r%s  └─ Syncing: [%s] %3.0f%% (%d/%d) %-25s", prefix, bar, percent, current, totalFiles, fileName)

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
			fmt.Print("\n")
		}

		return nil
	})
}

func (m *Manager) Run() error {
	if err := m.detectEnv(); err != nil {
		return err
	}

	// 定义日志前缀
	prefix := fmt.Sprintf("[%s] ", m.nodeCfg.IP)
	fmt.Printf("%sDetected: %s | Arch: %s | GPU: %v\n", prefix, m.installer.Name(), m.context.Arch, m.context.HasGPU)

	steps := []runner.Step{
		{
			Name: "分发离线资源",
			Check: func() (bool, error) {
				if _, err := m.client.RunCommand(fmt.Sprintf("test -d %q", m.context.RemoteTmpDir)); err == nil {
					return true, nil // 存在
				}
				return false, nil // 不存在
			},
			Action: m.distributeResources,
		},
		{
			Name:   "禁用 SELinux",
			Check:  m.installer.CheckSELinux,
			Action: m.installer.DisableSELinux,
		},
		{
			Name:   "禁用 Firewall",
			Check:  m.installer.CheckFirewall,
			Action: m.installer.DisableFirewall,
		},
		{
			Name:   "禁用 Swap分区",
			Check:  m.installer.CheckSwap,
			Action: m.installer.DisableSwap,
		},
		{
			Name:   "加载内核模块",
			Check:  m.installer.CheckKernelModules,
			Action: m.installer.LoadKernelModules,
		},
		{
			Name:   "配置 Sysctl 内核参数",
			Check:  m.installer.CheckSysctl,
			Action: m.installer.ConfigureSysctl,
		},
		{
			Name: "安装常用工具",
			Check: func() (bool, error) {
				if !m.nodeCfg.InstallTools {
					return true, nil
				}
				return m.installer.CheckCommonTools()
			},
			Action: m.installer.InstallCommonTools,
		},
		{
			Name:   "安装 Containerd 二进制文件",
			Check:  m.installer.CheckContainerdBinaries,
			Action: m.installer.InstallContainerdBinaries,
		},
		{
			Name:   "安装 Runc",
			Check:  m.installer.CheckRunc,
			Action: m.installer.InstallRunc,
		},
		{
			Name:   "配置 Containerd 服务",
			Check:  m.installer.CheckContainerdService,
			Action: m.installer.ConfigureContainerdService,
		},
		{
			Name:   "配置cgroup、私有镜像仓库并启动 Containerd",
			Check:  m.installer.CheckContainerdRunning,
			Action: m.installer.ConfigureAndStartContainerd,
		},
		{
			Name:   "配置 Crictl 默认endpoint",
			Check:  m.installer.CheckCrictl,
			Action: m.installer.ConfigureCrictl,
		},
		{
			Name:   "安装 Nerdctl",
			Check:  m.installer.CheckNerdctl,
			Action: m.installer.InstallNerdctl,
		},
		{
			Name:   "配置 GPU 运行时",
			Check:  m.installer.CheckGPUConfig,
			Action: m.installer.ConfigureGPU,
		},
		{
			Name:   "安装 Kubernetes 组件",
			Check:  m.installer.CheckK8sComponents,
			Action: m.installer.InstallK8sComponents,
		},
		{
			Name:   "加载镜像",
			Check:  m.installer.CheckImages,
			Action: m.installer.LoadImages,
		},
		{
			Name:   "初始化或加入集群",
			Check:  m.checkClusterStatus,
			Action: m.runKubeadm,
		},
	}

	// 调用 Runner，传入前缀
	return runner.RunPipeline(steps, prefix)
}

func (m *Manager) checkClusterStatus() (bool, error) {
	out, err := m.client.RunCommand("ls /etc/kubernetes/admin.conf")
	// 注意：对于 Worker 节点，可能没有 admin.conf，可以检查 kubelet.conf
	if !m.nodeCfg.IsMaster {
		out, err = m.client.RunCommand("ls /etc/kubernetes/kubelet.conf")
	}
	if err == nil && out != "" {
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
			repo = fmt.Sprintf("%s/google_containers", m.globalCfg.Registry.Endpoint)
		}

		cmd := fmt.Sprintf(`kubeadm init --v 0 \
--kubernetes-version=v%s \
--image-repository=%s`, m.globalCfg.Versions.K8s, repo)

		out, err := m.client.RunCommand(cmd)
		if err != nil {
			return err
		}
		fmt.Printf("[%s] Init Result:\n%s\n", m.nodeCfg.IP, out)

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
	fmt.Printf("[%s] Token Create Result:\n%s\n", m.nodeCfg.IP, out)
	m.globalCfg.JoinCommand = out
	return nil
}
