package config

type Config struct {
	// 全局配置
	Registry RegistryConfig `yaml:"registry"`
	Versions VersionConfig  `yaml:"versions"`
	Addons   AddonsConfig   `yaml:"addons"`
	HA       HAConfig       `yaml:"ha"`

	// 默认 SSH 配置 (如果 Node 中未指定则使用此默认值)
	SSHPort int    `yaml:"ssh_port"`
	User    string `yaml:"user"`
	// 命令执行超时（秒）
	CommandTimeoutSeconds int `yaml:"command_timeout_seconds"`
	// 安装模式：full(从零安装) 或 addons-only(仅部署组件)
	InstallMode string `yaml:"install_mode"`

	// 节点列表
	Nodes             []NodeConfig `yaml:"nodes"`
	JoinCommand       string       `yaml:"join_command"`        // 供 Worker 节点使用的全局 Join 命令
	MasterJoinCommand string       `yaml:"master_join_command"` // 供 Master 节点使用的 Join 命令

	// 仅执行预检查，不执行安装动作
	DryRun bool `yaml:"dry_run"`
}

type NodeConfig struct {
	IP              string `yaml:"ip"`
	Password        string `yaml:"password"`
	SSHPort         int    `yaml:"ssh_port"` // 可选：覆盖全局 Port
	IsMaster        bool   `yaml:"is_master"`
	IsPrimaryMaster bool   `yaml:"is_primary_master"`
	Interface       string `yaml:"interface"`
}

type RegistryConfig struct {
	Endpoint string `yaml:"endpoint"`
	IP       string `yaml:"ip"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type VersionConfig struct {
	DockerCE   string `yaml:"dockerce"`
	Containerd string `yaml:"containerd"`
	Runc       string `yaml:"runc"`
	Nerdctl    string `yaml:"nerdctl"`
	K8s        string `yaml:"k8s"`
}

type AddonsConfig struct {
	KubeOvn        AddonComponentConfig `yaml:"kube_ovn"`
	MultusCNI      AddonComponentConfig `yaml:"multus_cni"`
	Hami           AddonComponentConfig `yaml:"hami"`
	KubePrometheus AddonComponentConfig `yaml:"kube_prometheus_stack"`
}

type AddonComponentConfig struct {
	Enabled bool   `yaml:"enabled"`
	Version string `yaml:"version"`
}

type HAConfig struct {
	Enabled   bool   `yaml:"enabled"`
	VirtualIP string `yaml:"virtual_ip"`
}
