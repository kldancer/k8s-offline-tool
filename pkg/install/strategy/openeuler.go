package strategy

import (
	"fmt"
	"strings"
)

type OpenEulerInstaller struct {
	Ctx *Context
}

func (o *OpenEulerInstaller) Name() string            { return "openEuler" }
func (o *OpenEulerInstaller) verPath(v string) string { return strings.ReplaceAll(v, ".", "-") }

// --- System Prep ---
func (o *OpenEulerInstaller) CheckSELinux() (bool, error) {
	out, _ := o.Ctx.RunCmd("getenforce")
	return strings.Contains(strings.ToLower(out), "disabled") || strings.Contains(strings.ToLower(out), "permissive"), nil
}
func (o *OpenEulerInstaller) DisableSELinux() error {
	o.Ctx.RunCmd("sed -ri 's/SELINUX=enforcing/SELINUX=disabled/' /etc/selinux/config")
	o.Ctx.RunCmd("setenforce 0 || true")
	return nil
}
func (o *OpenEulerInstaller) CheckFirewall() (bool, error) {
	out, _ := o.Ctx.RunCmd("systemctl is-active firewalld")
	return strings.TrimSpace(out) == "inactive" || strings.Contains(out, "unknown"), nil
}
func (o *OpenEulerInstaller) DisableFirewall() error {
	o.Ctx.RunCmd("systemctl stop firewalld || true")
	o.Ctx.RunCmd("systemctl disable firewalld || true")
	return nil
}
func (o *OpenEulerInstaller) CheckSwap() (bool, error) {
	return CheckSwap(o.Ctx)
}
func (o *OpenEulerInstaller) DisableSwap() error {
	o.Ctx.RunCmd("swapoff -a")
	o.Ctx.RunCmd("sed -i '/swap/s/^/#/' /etc/fstab")
	return nil
}
func (o *OpenEulerInstaller) CheckKernelModules() (bool, error) {
	return CheckKernelModules(o.Ctx)
}
func (o *OpenEulerInstaller) LoadKernelModules() error {
	if err := LoadKernelModules(o.Ctx); err != nil {
		return err
	}
	return o.ConfigureSysctl()
}
func (o *OpenEulerInstaller) CheckSysctl() (bool, error) {
	commonOK, err := CheckSysctl(o.Ctx)
	if err != nil || !commonOK {
		return commonOK, err
	}

	_, err = o.Ctx.RunCmd("cat /etc/sysctl.d/99-sysctl.conf | grep net.ipv4.ip_forward=1")
	return err == nil, nil
}
func (o *OpenEulerInstaller) ConfigureSysctl() error {
	if err := ConfigureSysctl(o.Ctx); err != nil {
		return err
	}

	cmd := `sed -ri '/^[[:space:]]*net\.ipv4\.ip_forward[[:space:]]*=/d' /etc/sysctl.d/99-sysctl.conf &&
echo 'net.ipv4.ip_forward=1' >> /etc/sysctl.d/99-sysctl.conf &&
sysctl --system`
	_, err := o.Ctx.RunCmd(cmd)
	return err
}

// --- Tools ---
func (o *OpenEulerInstaller) CheckCommonTools() (bool, error) {
	// 欧拉系统暂时无需安装额外的工具
	return true, nil
}
func (o *OpenEulerInstaller) InstallCommonTools() error {
	// 欧拉系统暂时无需安装额外的工具
	return nil
}

// --- Load Balancer ---
func (o *OpenEulerInstaller) CheckHAProxy() (bool, error) {
	out, err := o.Ctx.RunCmd("haproxy -v")
	return err == nil && strings.Contains(strings.ToLower(out), "haproxy"), nil
}

func (o *OpenEulerInstaller) InstallHAProxy() error {
	rpmPath := fmt.Sprintf("%s/ha/haproxy/%s/rpm/*.rpm", o.Ctx.RemoteTmpDir, o.Ctx.Arch)
	_, err := o.Ctx.RunCmd(fmt.Sprintf("sudo dnf install -y %s --disablerepo=\"*\" --nogpgcheck", rpmPath))
	return err
}

func (o *OpenEulerInstaller) CheckKeepalived() (bool, error) {
	out, err := o.Ctx.RunCmd("keepalived -v")
	return err == nil && strings.Contains(strings.ToLower(out), "keepalived"), nil
}

func (o *OpenEulerInstaller) InstallKeepalived() error {
	rpmPath := fmt.Sprintf("%s/ha/keepalived/%s/rpm/*.rpm", o.Ctx.RemoteTmpDir, o.Ctx.Arch)
	_, err := o.Ctx.RunCmd(fmt.Sprintf("sudo dnf install -y %s --disablerepo=\"*\" --nogpgcheck", rpmPath))
	return err
}

// --- Containerd Granular ---
func (o *OpenEulerInstaller) CheckDockerCEPackage() (bool, error) {
	return CheckDockerCEPackage(o.Ctx)
}
func (o *OpenEulerInstaller) InstallDockerCEPackage() error {
	return InstallDockerCEBinary(o.Ctx)
}

func (o *OpenEulerInstaller) CheckContainerdRunning() (bool, error) {
	return CheckContainerdRunning(o.Ctx)
}

func (o *OpenEulerInstaller) ConfigureAndStartContainerd() error {
	return ConfigureAndStartContainerd(o.Ctx)
}

func (o *OpenEulerInstaller) CheckConfiguraRegistryContainerd() (bool, error) {
	return CheckConfiguraRegistryContainerd(o.Ctx)
}

func (o *OpenEulerInstaller) ConfiguraRegistryContainerd() error {
	return ConfiguraRegistryContainerd(o.Ctx)
}

func (o *OpenEulerInstaller) CheckCrictl() (bool, error) {
	return CheckCrictl(o.Ctx)
}

func (o *OpenEulerInstaller) ConfigureCrictl() error {
	return ConfigureCrictl(o.Ctx)
}

func (o *OpenEulerInstaller) CheckNerdctl() (bool, error) {
	return CheckNerdctl(o.Ctx)
}

func (o *OpenEulerInstaller) InstallNerdctl() error {
	vFolder := o.verPath(o.Ctx.Cfg.Versions.Nerdctl)
	return InstallNerdctl(o.Ctx, vFolder)
}

// --- Accelerators ---
func (o *OpenEulerInstaller) CheckAcceleratorConfig() (bool, error) {
	return CheckAcceleratorConfig(o.Ctx)
}

func (o *OpenEulerInstaller) ConfigureAccelerator() error {
	if o.Ctx.HasGPU {
		rpmPath := fmt.Sprintf("%s/common-tools/%s/rpm/nvidia-container-toolkit*.rpm", o.Ctx.RemoteTmpDir, o.Ctx.Arch)
		o.Ctx.RunCmd(fmt.Sprintf("rpm -Uvh %s --nodeps --force", rpmPath))
		o.Ctx.RunCmd("nvidia-ctk runtime configure --runtime=containerd")
		// default_runtime_name 改为 nvidia
		o.Ctx.RunCmd("sed -i 's/^\\([[:space:]]*default_runtime_name[[:space:]]*=[[:space:]]*\\)\"runc\"/\\1\"nvidia\"/' /etc/containerd/conf.d/99-nvidia.toml")
		o.Ctx.RunCmd("systemctl restart containerd")
	}

	if o.Ctx.HasNPU {
		runtimeDir := fmt.Sprintf("%s/docker-runtime/ascend/%s", o.Ctx.RemoteTmpDir, o.Ctx.Arch)
		installCmd := fmt.Sprintf("cd %s && ./*.run --install", runtimeDir)
		if _, err := o.Ctx.RunCmd(installCmd); err != nil {
			return fmt.Errorf("failed to install ascend docker runtime: %v", err)
		}

		out, err := o.Ctx.RunCmd("cat /etc/containerd/config.toml | grep ascend-docker-runtime")
		if err != nil || !strings.Contains(out, "ascend-docker-runtime") {
			return fmt.Errorf("failed to verify ascend docker runtime installation: %v", err)
		}
		o.Ctx.RunCmd("systemctl restart containerd")
	}

	return nil
}

// --- K8s ---
func (o *OpenEulerInstaller) CheckK8sComponents() (bool, error) {
	out, err := o.Ctx.RunCmd("kubeadm version -o short")
	if err != nil {
		return false, nil
	}
	return strings.Contains(out, o.Ctx.Cfg.Versions.K8s), nil
}
func (o *OpenEulerInstaller) InstallK8sComponents() error {
	vFolder := o.verPath(o.Ctx.Cfg.Versions.K8s)
	rpmPath := fmt.Sprintf("%s/k8s/%s/rpm/%s/*.rpm", o.Ctx.RemoteTmpDir, o.Ctx.Arch, vFolder)
	_, err := o.Ctx.RunCmd(fmt.Sprintf("rpm -Uvh %s --nodeps --force", rpmPath))
	o.Ctx.RunCmd("systemctl enable --now kubelet")
	o.Ctx.RunCmd("systemctl start kubelet")
	return err
}
