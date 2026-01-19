package strategy

import (
	"fmt"
	"strings"
)

type FedoraInstaller struct {
	Ctx *Context
}

func (f *FedoraInstaller) Name() string            { return "Fedora/CentOS" }
func (f *FedoraInstaller) verPath(v string) string { return strings.ReplaceAll(v, ".", "-") }

// --- System Prep ---
func (f *FedoraInstaller) CheckSELinux() (bool, error) {
	out, _ := f.Ctx.RunCmd("getenforce")
	return strings.Contains(strings.ToLower(out), "disabled") || strings.Contains(strings.ToLower(out), "permissive"), nil
}
func (f *FedoraInstaller) DisableSELinux() error {
	f.Ctx.RunCmd("sed -ri 's/SELINUX=enforcing/SELINUX=disabled/' /etc/selinux/config")
	f.Ctx.RunCmd("setenforce 0 || true")
	return nil
}
func (f *FedoraInstaller) CheckFirewall() (bool, error) {
	out, _ := f.Ctx.RunCmd("systemctl is-active firewalld")
	return strings.TrimSpace(out) == "inactive" || strings.Contains(out, "unknown"), nil
}
func (f *FedoraInstaller) DisableFirewall() error {
	f.Ctx.RunCmd("systemctl stop firewalld || true")
	f.Ctx.RunCmd("systemctl disable firewalld || true")
	return nil
}
func (f *FedoraInstaller) CheckSwap() (bool, error) {
	out, _ := f.Ctx.RunCmd("swapon --show")
	return strings.TrimSpace(out) == "", nil
}
func (f *FedoraInstaller) DisableSwap() error {
	f.Ctx.RunCmd("dnf remove -y zram-generator-defaults || true")
	f.Ctx.RunCmd("swapoff -a")
	return nil
}
func (f *FedoraInstaller) CheckKernelModules() (bool, error) {
	out, err := f.Ctx.RunCmd("cat /etc/modules-load.d/containerd.conf")
	return err == nil && strings.Contains(out, "overlay"), nil
}
func (f *FedoraInstaller) LoadKernelModules() error {
	f.Ctx.RunCmd(`cat > /etc/modules-load.d/containerd.conf << EOF
overlay
br_netfilter
EOF`)
	f.Ctx.RunCmd("modprobe overlay")
	f.Ctx.RunCmd("modprobe br_netfilter")
	return nil
}
func (f *FedoraInstaller) CheckSysctl() (bool, error) {
	out, err := f.Ctx.RunCmd("cat /etc/sysctl.d/99-kubernetes-cri.conf")
	return err == nil && strings.Contains(out, "net.ipv4.ip_forward"), nil
}
func (f *FedoraInstaller) ConfigureSysctl() error {
	f.Ctx.RunCmd(`cat > /etc/sysctl.d/99-kubernetes-cri.conf << EOF
net.bridge.bridge-nf-call-iptables  = 1
net.ipv4.ip_forward                 = 1
net.bridge.bridge-nf-call-ip6tables = 1
EOF`)
	f.Ctx.RunCmd("sysctl --system")
	return nil
}

// --- Tools ---
func (f *FedoraInstaller) CheckCommonTools() (bool, error) {
	//if _, err := f.Ctx.RunCmd("rpm -q htop"); err != nil {
	//	return false, nil
	//}
	// 暂时默认重新装一遍就行
	return false, nil
}
func (f *FedoraInstaller) InstallCommonTools() error {
	rpmPath := fmt.Sprintf("%s/common-tools/%s/rpm/*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("rpm -Uvh %s --nodeps --force", rpmPath))
	return err
}

// --- Containerd Granular ---
func (f *FedoraInstaller) CheckContainerdBinaries() (bool, error) {
	out, err := f.Ctx.RunCmd("containerd --version")
	// 检查版本是否包含配置的版本号
	return err == nil && strings.Contains(out, f.Ctx.Cfg.Versions.Containerd), nil
}
func (f *FedoraInstaller) InstallContainerdBinaries() error {
	vFolder := f.verPath(f.Ctx.Cfg.Versions.Containerd)
	tarCmd := fmt.Sprintf("tar -C /usr/local -xzf %s/containerd/%s/%s/containerd-%s-linux-%s.tar.gz",
		f.Ctx.RemoteTmpDir, f.Ctx.Arch, vFolder, f.Ctx.Cfg.Versions.Containerd, f.Ctx.Arch)
	_, err := f.Ctx.RunCmd(tarCmd)
	return err
}

func (f *FedoraInstaller) CheckRunc() (bool, error) {
	out, err := f.Ctx.RunCmd("runc --version")
	return err == nil && strings.Contains(out, f.Ctx.Cfg.Versions.Runc), nil
}
func (f *FedoraInstaller) InstallRunc() error {
	vFolder := f.verPath(f.Ctx.Cfg.Versions.Runc)
	runcPath := fmt.Sprintf("%s/runc/%s/%s/runc.%s", f.Ctx.RemoteTmpDir, f.Ctx.Arch, vFolder, f.Ctx.Arch)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("install -m 0755 %s /usr/local/bin/runc", runcPath))
	return err
}

func (f *FedoraInstaller) CheckContainerdService() (bool, error) {
	if _, err := f.Ctx.RunCmd(fmt.Sprintf("test -d %q", "/usr/lib/systemd/system/containerd.service")); err == nil {
		return true, nil // 存在
	}
	return false, nil // 不存在
}

func (f *FedoraInstaller) ConfigureContainerdService() error {
	svcSrc := fmt.Sprintf("%s/containerd/containerd.service", f.Ctx.RemoteTmpDir)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("cp %s /usr/lib/systemd/system/containerd.service", svcSrc))
	return err
}

func (f *FedoraInstaller) CheckContainerdRunning() (bool, error) {
	out, _ := f.Ctx.RunCmd("systemctl is-active containerd")
	return strings.TrimSpace(out) == "active", nil
}

func (f *FedoraInstaller) ConfigureAndStartContainerd() error {
	// 1. 生成默认配置
	f.Ctx.RunCmd("mkdir -p /etc/containerd")
	f.Ctx.RunCmd("containerd config default > /etc/containerd/config.toml")

	// 2. 修改 SystemdCgroup
	f.Ctx.RunCmd("sed -i 's/SystemdCgroup = false/SystemdCgroup = true/g' /etc/containerd/config.toml")

	// 3. 配置 Registry
	if f.Ctx.Cfg.Registry.Endpoint != "" {
		// 3.1 启用 certs.d 目录配置
		f.Ctx.RunCmd("sed -i \"s|config_path = '/etc/containerd/certs.d:/etc/docker/certs.d'|config_path = '/etc/containerd/certs.d'|g\" /etc/containerd/config.toml")
		// 3.2 配置 hosts.toml
		regDomain := f.Ctx.Cfg.Registry.Endpoint + fmt.Sprintf(":%d", f.Ctx.Cfg.Registry.Port)

		// 3.3 修改 sandbox镜像
		f.Ctx.RunCmd(fmt.Sprintf("sed -i \"s|sandbox = 'registry.k8s.io/pause:3.10.1'|sandbox = '%S/google_containers/pause:3.10.1'|g\" /etc/containerd/config.toml",
			regDomain))

		// 3.4 添加域名解析配置
		f.Ctx.RunCmd(fmt.Sprintf(" echo \" %s %s\" | sudo tee -a /etc/hosts", f.Ctx.Cfg.Registry.IP, f.Ctx.Cfg.Registry.Endpoint))

		regUrl := "http://" + regDomain

		// 创建目录
		f.Ctx.RunCmd(fmt.Sprintf("mkdir -p /etc/containerd/certs.d/%s", regDomain))

		// 写入 hosts.toml
		hostsToml := fmt.Sprintf(`server = "%s"

[host."%s"]
  capabilities = ["pull", "resolve", "push"]
`, regUrl, regUrl)

		cmd := fmt.Sprintf("cat > /etc/containerd/certs.d/%s/hosts.toml <<EOF\n%s\nEOF", regDomain, hostsToml)
		if _, err := f.Ctx.RunCmd(cmd); err != nil {
			return fmt.Errorf("failed to write hosts.toml: %v", err)
		}

	}

	// 4. 启动服务
	f.Ctx.RunCmd("systemctl daemon-reload")
	_, err := f.Ctx.RunCmd("systemctl enable --now containerd")
	return err
}

func (f *FedoraInstaller) CheckCrictl() (bool, error) {
	out, err := f.Ctx.RunCmd("cat /etc/crictl.yaml")
	return err == nil && strings.Contains(out, "containerd.sock"), nil
}

func (f *FedoraInstaller) ConfigureCrictl() error {
	cmd := `cat > /etc/crictl.yaml << EOF
runtime-endpoint: unix:///run/containerd/containerd.sock
image-endpoint: unix:///run/containerd/containerd.sock
timeout: 2
debug: false
pull-image-on-create: false
EOF`
	_, err := f.Ctx.RunCmd(cmd)
	return err
}

func (f *FedoraInstaller) CheckNerdctl() (bool, error) {
	out, err := f.Ctx.RunCmd("nerdctl --version")
	return err == nil && strings.Contains(out, f.Ctx.Cfg.Versions.Nerdctl), nil
}

func (f *FedoraInstaller) InstallNerdctl() error {
	vFolder := f.verPath(f.Ctx.Cfg.Versions.Nerdctl)
	tarPath := fmt.Sprintf("%s/nerdctl/%s/%s/nerdctl-%s-linux-%s.tar.gz",
		f.Ctx.RemoteTmpDir, f.Ctx.Arch, vFolder, f.Ctx.Cfg.Versions.Nerdctl, f.Ctx.Arch)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("tar -xzf %s -C /usr/local/bin/", tarPath))
	return err
}

// --- GPU ---
func (f *FedoraInstaller) CheckGPUConfig() (bool, error) {
	if !f.Ctx.HasGPU {
		return true, nil
	}
	_, err := f.Ctx.RunCmd("nvidia-container-cli info")
	return err == nil, nil
}

func (f *FedoraInstaller) ConfigureGPU() error {
	rpmPath := fmt.Sprintf("%s/common-tools/%s/rpm/nvidia-container-toolkit*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch)
	f.Ctx.RunCmd(fmt.Sprintf("rpm -Uvh %s --nodeps --force", rpmPath))
	f.Ctx.RunCmd("nvidia-ctk runtime configure --runtime=containerd")
	f.Ctx.RunCmd("systemctl restart containerd")
	return nil
}

// --- K8s ---
func (f *FedoraInstaller) CheckK8sComponents() (bool, error) {
	// 暂时不检查，直接覆盖安装
	return false, nil
}
func (f *FedoraInstaller) InstallK8sComponents() error {
	vFolder := f.verPath(f.Ctx.Cfg.Versions.K8s)
	// Path example: k8s/1-35-0/rpm/1-35-0/*.rpm
	rpmPath := fmt.Sprintf("%s/k8s/%s/rpm/%s/*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch, vFolder)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("rpm -Uvh %s --nodeps --force", rpmPath))
	f.Ctx.RunCmd("systemctl enable --now kubelet")
	f.Ctx.RunCmd("systemctl start kubelet")
	return err
}

func (f *FedoraInstaller) CheckImages() (bool, error) {
	if f.Ctx.Cfg.Registry.Endpoint != "" {
		// 如果配置了私有仓库，默认跳过检查
		return true, nil
	}
	out, _ := f.Ctx.RunCmd("ctr -n k8s.io images list | grep kube-apiserver")
	return strings.TrimSpace(out) != "", nil
}
func (f *FedoraInstaller) LoadImages() error {
	cmd := fmt.Sprintf("find %s/images -name '*.tar' -exec ctr -n k8s.io images import {} \\;", f.Ctx.RemoteTmpDir)
	_, err := f.Ctx.RunCmd(cmd)
	return err
}
