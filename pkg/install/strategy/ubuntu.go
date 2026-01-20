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
	return CheckSwap(u.Ctx)
}
func (u *UbuntuInstaller) DisableSwap() error {
	u.Ctx.RunCmd("swapoff -a")
	u.Ctx.RunCmd("sed -i '/\\/swap.img/s/^/#/' /etc/fstab")
	return nil
}
func (u *UbuntuInstaller) CheckKernelModules() (bool, error) {
	return CheckKernelModules(u.Ctx)
}
func (u *UbuntuInstaller) LoadKernelModules() error {
	return LoadKernelModules(u.Ctx)
}
func (u *UbuntuInstaller) CheckSysctl() (bool, error) {
	return CheckSysctl(u.Ctx)
}
func (u *UbuntuInstaller) ConfigureSysctl() error {
	return ConfigureSysctl(u.Ctx)
}

// --- Tools ---
func (u *UbuntuInstaller) CheckCommonTools() (bool, error) {
	//if _, err := u.Ctx.RunCmd("dpkg -s htop"); err != nil {
	//	return false, nil
	//}
	// 暂时默认重新装一遍就行
	return false, nil
}
func (u *UbuntuInstaller) InstallCommonTools() error {
	debPath := fmt.Sprintf("%s/common-tools/%s/apt/*.deb", u.Ctx.RemoteTmpDir, u.Ctx.Arch)
	_, err := u.Ctx.RunCmd(fmt.Sprintf("dpkg -i %s", debPath))
	return err
}

// --- Containerd Granular ---
func (u *UbuntuInstaller) CheckContainerdBinaries() (bool, error) {
	return CheckContainerdBinaries(u.Ctx)
}
func (u *UbuntuInstaller) InstallContainerdBinaries() error {
	vFolder := u.verPath(u.Ctx.Cfg.Versions.Containerd)
	return InstallContainerdBinaries(u.Ctx, vFolder)
}

func (u *UbuntuInstaller) CheckRunc() (bool, error) {
	return CheckRunc(u.Ctx)
}
func (u *UbuntuInstaller) InstallRunc() error {
	vFolder := u.verPath(u.Ctx.Cfg.Versions.Runc)
	return InstallRunc(u.Ctx, vFolder)
}

func (u *UbuntuInstaller) CheckContainerdService() (bool, error) {
	return CheckContainerdService(u.Ctx)
}

func (u *UbuntuInstaller) ConfigureContainerdService() error {
	return ConfigureContainerdService(u.Ctx)
}

func (u *UbuntuInstaller) CheckContainerdRunning() (bool, error) {
	return CheckContainerdRunning(u.Ctx)
}
func (u *UbuntuInstaller) ConfigureAndStartContainerd() error {
	return ConfigureAndStartContainerd(u.Ctx)
}

func (u *UbuntuInstaller) CheckCrictl() (bool, error) {
	return CheckCrictl(u.Ctx)
}
func (u *UbuntuInstaller) ConfigureCrictl() error {
	return ConfigureCrictl(u.Ctx)
}

func (u *UbuntuInstaller) CheckNerdctl() (bool, error) {
	return CheckNerdctl(u.Ctx)
}
func (u *UbuntuInstaller) InstallNerdctl() error {
	vFolder := u.verPath(u.Ctx.Cfg.Versions.Nerdctl)
	return InstallNerdctl(u.Ctx, vFolder)
}

// --- GPU ---
func (u *UbuntuInstaller) CheckGPUConfig() (bool, error) {
	return CheckGPUConfig(u.Ctx)
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
