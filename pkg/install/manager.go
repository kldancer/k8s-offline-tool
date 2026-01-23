package install

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
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

var hasMirrorSync = false

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
	}

	if m.globalCfg.Registry.Endpoint != "" {
		steps = append(steps,
			runner.Step{
				Name:   "配置私有镜像仓库,并重启 Containerd",
				Check:  m.installer.CheckConfiguraRegistryContainerd,
				Action: m.installer.ConfiguraRegistryContainerd,
			},
		)

		// full模式、addons模式下，需要同步镜像, 且同步镜像的操作只执行一次
		if m.globalCfg.InstallMode != config.InstallModeInstallOnly && !hasMirrorSync {
			steps = append(steps,
				runner.Step{
					Name: "同步镜像到私有仓库",
					Check: func() (bool, error) {
						return false, nil
					},
					Action: m.syncImagesToRegistry,
				},
			)
		}
	}

	if m.context.HasGPU {
		steps = append(steps,
			runner.Step{
				Name:   "配置 GPU 运行时",
				Check:  m.installer.CheckGPUConfig,
				Action: m.installer.ConfigureGPU,
			},
		)
	}

	if m.globalCfg.InstallMode != config.InstallModeAddonsOnly {
		// 非 addons模式下，需要安装Kubernetes 组件
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

	if m.nodeCfg.IsMaster {
		steps = append(steps, m.addonSteps()...)
	}

	// 调用 Runner，传入前缀
	return runner.RunPipeline(steps, prefix, m.output, dryRun)
}

func (m *Manager) addonSteps() []runner.Step {
	return []runner.Step{
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
	if _, err := m.runLocalCmd("docker --version"); err == nil {
		return "docker", nil
	}
	if _, err := m.runLocalCmd("nerdctl --version"); err == nil {
		return "nerdctl", nil
	}
	if _, err := m.runLocalCmd("ctr version"); err == nil {
		return "ctr", nil
	}
	return "", fmt.Errorf("docker, nerdctl, or ctr is required to sync images")
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
	images := config.RequiredK8sImages

	if m.globalCfg.Addons.KubeOvn.Enabled {
		images = append(images, config.RequiredKubeOvnImages...)
	}
	if m.globalCfg.Addons.MultusCNI.Enabled {
		images = append(images, config.RequiredMultusCNImages...)
	}
	if m.globalCfg.Addons.LocalPathStorage.Enabled {
		images = append(images, config.RequiredLocalPathProvisionerImages...)
	}

	type imageSyncItem struct {
		source   string
		target   string
		project  string
		repoName string
		tag      string
	}

	projectCache := make(map[string]bool)
	syncList := make([]imageSyncItem, 0, len(images))
	for _, image := range images {
		target := replaceImageRegistry(image, registryHost)
		repo, tag := splitImage(target)
		repoPath := strings.TrimPrefix(repo, registryHost+"/")
		project, repoName, err := splitHarborRepository(repoPath)
		if err != nil {
			return err
		}
		exists, ok := projectCache[project]
		if !ok {
			exists, err = m.harborProjectExists(registryHost, project)
			if err != nil {
				return err
			}
			projectCache[project] = exists
		}
		if exists {
			tagExists, err := m.harborTagExists(registryHost, project, repoName, tag)
			if err != nil {
				return err
			}
			if tagExists {
				continue
			}
		}
		syncList = append(syncList, imageSyncItem{
			source:   image,
			target:   target,
			project:  project,
			repoName: repoName,
			tag:      tag,
		})
	}

	prefix := fmt.Sprintf("[%s] ", m.nodeCfg.IP)
	if len(syncList) == 0 {
		fmt.Fprintf(m.output, "%s  └─ 镜像已全部同步，无需重复同步\n", prefix)
		hasMirrorSync = true
		return nil
	}

	fmt.Fprintf(m.output, "%s  └─ 需要同步镜像（%d）：\n", prefix, len(syncList))
	for _, item := range syncList {
		fmt.Fprintf(m.output, "%s    - %s\n", prefix, item.source)
	}

	current := 0
	total := len(syncList)
	const barWidth = 20

	for _, item := range syncList {
		if !projectCache[item.project] {
			if err := m.createHarborProject(registryHost, item.project); err != nil {
				return err
			}
			projectCache[item.project] = true
		}

		displayIndex := current + 1
		percent := float64(displayIndex) / float64(total) * 100
		filled := int(float64(barWidth) * float64(displayIndex) / float64(total))
		if filled > barWidth {
			filled = barWidth
		}
		bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
		if filled > 0 && filled < barWidth {
			bar = strings.Repeat("=", filled-1) + ">" + strings.Repeat(" ", barWidth-filled)
		}

		fmt.Fprintf(m.output, "\r%s  └─ Syncing: [%s] %3.0f%% (%d/%d) %s", prefix, bar, percent, displayIndex, total, item.source)

		switch client {
		case "docker":
			if _, err := m.runLocalCmd(fmt.Sprintf("docker pull --platform=linux/%s %s", m.context.Arch, item.source)); err != nil {
				return err
			}
			if _, err := m.runLocalCmd(fmt.Sprintf("docker tag %s %s", item.source, item.target)); err != nil {
				return err
			}
			if _, err := m.runLocalCmd(fmt.Sprintf("docker push %s", item.target)); err != nil {
				return err
			}
		case "nerdctl":
			if _, err := m.runLocalCmd(fmt.Sprintf("nerdctl pull --platform=linux/%s %s", m.context.Arch, item.source)); err != nil {
				return err
			}
			if _, err := m.runLocalCmd(fmt.Sprintf("nerdctl tag %s %s", item.source, item.target)); err != nil {
				return err
			}
			// 不用添加 --insecure-registry，因为已经配置了http
			if _, err := m.runLocalCmd(fmt.Sprintf("nerdctl push %s", item.target)); err != nil {
				return err
			}
		case "ctr":
			if _, err := m.runLocalCmd(fmt.Sprintf("ctr images pull --platform=linux/%s %s", m.context.Arch, item.source)); err != nil {
				return err
			}
			if _, err := m.runLocalCmd(fmt.Sprintf("ctr images tag %s %s", item.source, item.target)); err != nil {
				return err
			}
			if _, err := m.runLocalCmd(fmt.Sprintf("ctr images push --plain-http %s", item.target)); err != nil {
				return err
			}
		}
		current++
		if current == total {
			fmt.Fprint(m.output, "\n")
		}
	}
	hasMirrorSync = true
	return nil
}

func (m *Manager) harborAuthArgs() string {
	username := strings.TrimSpace(m.globalCfg.Registry.Username)
	if username == "" {
		return ""
	}
	creds := username + ":" + m.globalCfg.Registry.Password
	return fmt.Sprintf("-u %q", creds)
}

func (m *Manager) harborRequest(method, requestURL string, body []byte) (int, string, error) {
	authArgs := m.harborAuthArgs()
	bodyArgs := ""
	if len(body) > 0 {
		bodyArgs = fmt.Sprintf("-H 'Content-Type: application/json' -d %q", string(body))
	}
	cmd := fmt.Sprintf("curl -s -w 'HTTPSTATUS:%%{http_code}' -X %s %s %s %q", method, authArgs, bodyArgs, requestURL)
	out, err := m.runLocalCmd(cmd)
	if err != nil {
		return 0, "", err
	}
	statusIndex := strings.LastIndex(out, "HTTPSTATUS:")
	if statusIndex == -1 {
		return 0, "", fmt.Errorf("unexpected harbor response: %s", out)
	}
	bodyText := strings.TrimSpace(out[:statusIndex])
	statusText := strings.TrimSpace(out[statusIndex+len("HTTPSTATUS:"):])
	status, err := strconv.Atoi(statusText)
	if err != nil {
		return 0, "", fmt.Errorf("invalid harbor status: %s", statusText)
	}
	return status, bodyText, nil
}

func (m *Manager) harborProjectExists(registryHost, project string) (bool, error) {
	requestURL := fmt.Sprintf("http://%s/api/v2.0/projects?name=%s", registryHost, url.QueryEscape(project))
	status, body, err := m.harborRequest("GET", requestURL, nil)
	if err != nil {
		return false, err
	}
	if status == 401 || status == 403 {
		return false, fmt.Errorf("harbor authentication failed for project %s", project)
	}
	if status != 200 {
		return false, fmt.Errorf("harbor project query failed with status %d: %s", status, body)
	}
	var projects []map[string]interface{}
	if err := json.Unmarshal([]byte(body), &projects); err != nil {
		return false, fmt.Errorf("failed to parse harbor project response: %w", err)
	}
	return len(projects) > 0, nil
}

func (m *Manager) createHarborProject(registryHost, project string) error {
	payload := map[string]interface{}{
		"project_name": project,
		"metadata": map[string]string{
			"public": "true",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	requestURL := fmt.Sprintf("http://%s/api/v2.0/projects", registryHost)
	status, body, err := m.harborRequest("POST", requestURL, data)
	if err != nil {
		return err
	}
	if status == 201 || status == 409 {
		return nil
	}
	return fmt.Errorf("harbor project create failed with status %d: %s", status, body)
}

func (m *Manager) harborTagExists(registryHost, project, repoName, tag string) (bool, error) {
	requestURL := fmt.Sprintf(
		"http://%s/api/v2.0/projects/%s/repositories/%s/artifacts/%s",
		registryHost,
		url.PathEscape(project),
		url.PathEscape(repoName),
		url.PathEscape(tag),
	)
	status, body, err := m.harborRequest("GET", requestURL, nil)
	if err != nil {
		return false, err
	}
	if status == 200 {
		return true, nil
	}
	if status == 404 {
		return false, nil
	}
	return false, fmt.Errorf("harbor artifact query failed with status %d: %s", status, body)
}

func splitHarborRepository(repoPath string) (string, string, error) {
	parts := strings.SplitN(repoPath, "/", 2)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid harbor repository path: %s", repoPath)
	}
	return parts[0], parts[1], nil
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
	cmd := fmt.Sprintf("bash %s > /dev/null 2>&1", installScript)
	m.context.RunCmd(cmd)
	return nil
}

func (m *Manager) deployMultusCNI() error {
	if err := m.ensureAdminConf(); err != nil {
		return err
	}
	manifestPath := path.Join(m.context.RemoteTmpDir, "cni", "multus-cni", "multus-daemonset-thick.yml")
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
		cmd := fmt.Sprintf("sed -i 's|docker.io/rancher/local-path-provisioner:v0.0.34|%s|g' %s", image, manifestPath)
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
		strings.HasPrefix(p, "cni/multus-cni") ||
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
	if strings.HasPrefix(p, "cni/multus-cni") {
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

		m.client.RunCommand("mkdir -p $HOME/.kube && cp -f /etc/kubernetes/admin.conf $HOME/.kube/config && chown $(id -u):$(id -g) $HOME/.kube/config")

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
	//fmt.Fprintf(m.output, "\n[%s] Token Create Result: %s ", m.nodeCfg.IP, out)
	m.globalCfg.JoinCommand = out
	return nil
}
