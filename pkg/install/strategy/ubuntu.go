package strategy

import (
	"fmt"
	"strings"
)

type UbuntuInstaller struct {
	Ctx *Context
}

func (u *UbuntuInstaller) Name() string            { return "Ubuntu/Debian" }
func (u *UbuntuInstaller) verPath(v string) string { return strings.ReplaceAll(v, ".", "-") }

// --- System Prep ---
func (u *UbuntuInstaller) CheckSELinux() (bool, error) { return true, nil }
func (u *UbuntuInstaller) DisableSELinux() error       { return nil }
func (u *UbuntuInstaller) CheckFirewall() (bool, error) {
	out, _ := u.Ctx.RunCmd("ufw status")
	return strings.Contains(out, "inactive"), nil
}
func (u *UbuntuInstaller) DisableFirewall() error {
	u.Ctx.RunCmd("systemctl stop ufw || true")
	u.Ctx.RunCmd("systemctl disable ufw || true")
	return nil
}
func (u *UbuntuInstaller) CheckSwap() (bool, error) {
	out, _ := u.Ctx.RunCmd("swapon --show")
	return strings.TrimSpace(out) == "", nil
}
func (u *UbuntuInstaller) DisableSwap() error {
	u.Ctx.RunCmd("swapoff -a")
	u.Ctx.RunCmd("sed -i '/\\/swap.img/s/^/#/' /etc/fstab")
	return nil
}
func (u *UbuntuInstaller) CheckKernelModules() (bool, error) {
	out, err := u.Ctx.RunCmd("cat /etc/modules-load.d/containerd.conf")
	return err == nil && strings.Contains(out, "overlay"), nil
}
func (u *UbuntuInstaller) LoadKernelModules() error {
	u.Ctx.RunCmd(`cat > /etc/modules-load.d/containerd.conf << EOF
overlay
br_netfilter
EOF`)
	u.Ctx.RunCmd("modprobe overlay")
	u.Ctx.RunCmd("modprobe br_netfilter")
	return nil
}
func (u *UbuntuInstaller) CheckSysctl() (bool, error) {
	out, err := u.Ctx.RunCmd("cat /etc/sysctl.d/99-kubernetes-cri.conf")
	return err == nil && strings.Contains(out, "net.ipv4.ip_forward"), nil
}
func (u *UbuntuInstaller) ConfigureSysctl() error {
	u.Ctx.RunCmd(`cat > /etc/sysctl.d/99-kubernetes-cri.conf << EOF
net.bridge.bridge-nf-call-iptables  = 1
net.ipv4.ip_forward                 = 1
net.bridge.bridge-nf-call-ip6tables = 1
EOF`)
	u.Ctx.RunCmd("sysctl --system")
	return nil
}

// --- Tools ---
func (u *UbuntuInstaller) CheckCommonTools() (bool, error) {
	//if _, err := u.Ctx.RunCmd("dpkg -s htop"); err != nil {
	//	return false, nil
	//}
	// 暂时默认重新装一遍就行
	return true, nil
}
func (u *UbuntuInstaller) InstallCommonTools() error {
	debPath := fmt.Sprintf("%s/common-tools/%s/apt/*.deb", u.Ctx.RemoteTmpDir, u.Ctx.Arch)
	_, err := u.Ctx.RunCmd(fmt.Sprintf("dpkg -i %s", debPath))
	return err
}

// --- Containerd Granular ---
func (u *UbuntuInstaller) CheckContainerdBinaries() (bool, error) {
	out, err := u.Ctx.RunCmd("containerd --version")
	return err == nil && strings.Contains(out, u.Ctx.Cfg.Versions.Containerd), nil
}
func (u *UbuntuInstaller) InstallContainerdBinaries() error {
	vFolder := u.verPath(u.Ctx.Cfg.Versions.Containerd)
	tarCmd := fmt.Sprintf("tar -C /usr/local -xzf %s/containerd/%s/%s/containerd-%s-linux-%s.tar.gz",
		u.Ctx.RemoteTmpDir, u.Ctx.Arch, vFolder, u.Ctx.Cfg.Versions.Containerd, u.Ctx.Arch)
	_, err := u.Ctx.RunCmd(tarCmd)
	return err
}

func (u *UbuntuInstaller) CheckRunc() (bool, error) {
	out, err := u.Ctx.RunCmd("runc --version")
	return err == nil && strings.Contains(out, u.Ctx.Cfg.Versions.Runc), nil
}
func (u *UbuntuInstaller) InstallRunc() error {
	vFolder := u.verPath(u.Ctx.Cfg.Versions.Runc)
	runcPath := fmt.Sprintf("%s/runc/%s/%s/runc.%s", u.Ctx.RemoteTmpDir, u.Ctx.Arch, vFolder, u.Ctx.Arch)
	_, err := u.Ctx.RunCmd(fmt.Sprintf("install -m 0755 %s /usr/local/bin/runc", runcPath))
	return err
}

func (u *UbuntuInstaller) CheckContainerdService() (bool, error) {
	if _, err := u.Ctx.RunCmd(fmt.Sprintf("test -d %q", "/usr/lib/systemd/system/containerd.service")); err == nil {
		return true, nil // 存在
	}
	return false, nil // 不存在
}

func (u *UbuntuInstaller) ConfigureContainerdService() error {
	svcSrc := fmt.Sprintf("%s/containerd/containerd.service", u.Ctx.RemoteTmpDir)
	_, err := u.Ctx.RunCmd(fmt.Sprintf("cp %s /usr/lib/systemd/system/containerd.service", svcSrc))
	return err
}

func (u *UbuntuInstaller) CheckContainerdRunning() (bool, error) {
	out, _ := u.Ctx.RunCmd("systemctl is-active containerd")
	return strings.TrimSpace(out) == "active", nil
}
func (u *UbuntuInstaller) ConfigureAndStartContainerd() error {
	u.Ctx.RunCmd("mkdir -p /etc/containerd")
	u.Ctx.RunCmd("containerd config default > /etc/containerd/config.toml")
	u.Ctx.RunCmd("sed -i 's/SystemdCgroup = false/SystemdCgroup = true/g' /etc/containerd/config.toml")

	if u.Ctx.Cfg.Registry.Endpoint != "" {
		u.Ctx.RunCmd("sed -i \"s|config_path = '/etc/containerd/certs.d:/etc/docker/certs.d'|config_path = '/etc/containerd/certs.d'|g\" /etc/containerd/config.toml")
		// Configure certs
		regDomain := u.Ctx.Cfg.Registry.Endpoint + fmt.Sprintf(":%d", u.Ctx.Cfg.Registry.Port)
		u.Ctx.RunCmd(fmt.Sprintf("sed -i \"s|sandbox = 'registry.k8s.io/pause:3.10.1'|sandbox = '%S/google_containers/pause:3.10.1'|g\" /etc/containerd/config.toml",
			regDomain))

		// 3.4 添加域名解析配置
		u.Ctx.RunCmd(fmt.Sprintf(" echo \" %s %s\" | sudo tee -a /etc/hosts", u.Ctx.Cfg.Registry.IP, u.Ctx.Cfg.Registry.Endpoint))

		regUrl := "http://" + regDomain

		u.Ctx.RunCmd(fmt.Sprintf("mkdir -p /etc/containerd/certs.d/%s", regDomain))
		hostsToml := fmt.Sprintf(`server = "%s"

[host."%s"]
  capabilities = ["pull", "resolve", "push"]
`, regUrl, regUrl)

		cmd := fmt.Sprintf("cat > /etc/containerd/certs.d/%s/hosts.toml <<EOF\n%s\nEOF", regDomain, hostsToml)
		if _, err := u.Ctx.RunCmd(cmd); err != nil {
			return fmt.Errorf("failed to write hosts.toml: %v", err)
		}

	}
	u.Ctx.RunCmd("systemctl daemon-reload")
	_, err := u.Ctx.RunCmd("systemctl enable --now containerd")
	return err
}

func (u *UbuntuInstaller) CheckCrictl() (bool, error) {
	out, err := u.Ctx.RunCmd("cat /etc/crictl.yaml")
	return err == nil && strings.Contains(out, "containerd.sock"), nil
}
func (u *UbuntuInstaller) ConfigureCrictl() error {
	cmd := `cat > /etc/crictl.yaml << EOF
runtime-endpoint: unix:///run/containerd/containerd.sock
image-endpoint: unix:///run/containerd/containerd.sock
timeout: 2
debug: false
pull-image-on-create: false
EOF`
	_, err := u.Ctx.RunCmd(cmd)
	return err
}

func (u *UbuntuInstaller) CheckNerdctl() (bool, error) {
	out, err := u.Ctx.RunCmd("nerdctl --version")
	return err == nil && strings.Contains(out, u.Ctx.Cfg.Versions.Nerdctl), nil
}
func (u *UbuntuInstaller) InstallNerdctl() error {
	vFolder := u.verPath(u.Ctx.Cfg.Versions.Nerdctl)
	tarPath := fmt.Sprintf("%s/nerdctl/%s/%s/nerdctl-%s-linux-%s.tar.gz",
		u.Ctx.RemoteTmpDir, u.Ctx.Arch, vFolder, u.Ctx.Cfg.Versions.Nerdctl, u.Ctx.Arch)
	_, err := u.Ctx.RunCmd(fmt.Sprintf("tar -xzf %s -C /usr/local/bin/", tarPath))
	return err
}

// --- GPU ---
func (u *UbuntuInstaller) CheckGPUConfig() (bool, error) {
	if !u.Ctx.HasGPU {
		return true, nil
	}
	_, err := u.Ctx.RunCmd("nvidia-container-cli info")
	return err == nil, nil
}
func (u *UbuntuInstaller) ConfigureGPU() error {
	debPath := fmt.Sprintf("%s/common-tools/%s/apt/nvidia-container-toolkit*.deb", u.Ctx.RemoteTmpDir, u.Ctx.Arch)
	u.Ctx.RunCmd(fmt.Sprintf("dpkg -i %s", debPath))
	u.Ctx.RunCmd("nvidia-ctk runtime configure --runtime=containerd")
	u.Ctx.RunCmd("systemctl restart containerd")
	return nil
}

// --- K8s ---
func (u *UbuntuInstaller) CheckK8sComponents() (bool, error) {
	// 暂时不检查，直接覆盖安装
	return false, nil
}
func (u *UbuntuInstaller) InstallK8sComponents() error {
	vFolder := u.verPath(u.Ctx.Cfg.Versions.K8s)
	debPath := fmt.Sprintf("%s/k8s/%s/apt/%s/*.deb", u.Ctx.RemoteTmpDir, u.Ctx.Arch, vFolder)
	_, err := u.Ctx.RunCmd(fmt.Sprintf("dpkg -i %s", debPath))
	u.Ctx.RunCmd("systemctl enable --now kubelet")
	u.Ctx.RunCmd("systemctl start kubelet")
	return err
}

func (u *UbuntuInstaller) CheckImages() (bool, error) {
	if u.Ctx.Cfg.Registry.Endpoint != "" {
		// 如果配置了私有仓库，默认跳过检查
		return true, nil
	}
	out, _ := u.Ctx.RunCmd("ctr -n k8s.io images list | grep kube-apiserver")
	return strings.TrimSpace(out) != "", nil
}
func (u *UbuntuInstaller) LoadImages() error {
	cmd := fmt.Sprintf("find %s/images -name '*.tar' -exec ctr -n k8s.io images import {} \\;", u.Ctx.RemoteTmpDir)
	_, err := u.Ctx.RunCmd(cmd)
	return err
}
