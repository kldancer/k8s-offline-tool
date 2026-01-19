package config

type Config struct {
	// 全局配置
	Registry RegistryConfig `yaml:"registry"`
	Versions VersionConfig  `yaml:"versions"`

	// 默认 SSH 配置 (如果 Node 中未指定则使用此默认值)
	SSHPort int    `yaml:"ssh_port"`
	User    string `yaml:"user"`
	// 命令执行超时（秒）
	CommandTimeoutSeconds int `yaml:"command_timeout_seconds"`

	// 节点列表
	Nodes       []NodeConfig `yaml:"nodes"`
	JoinCommand string       `yaml:"join_command"` // 供 Worker 节点使用的全局 Join 命令
}

type NodeConfig struct {
	IP           string `yaml:"ip"`
	Password     string `yaml:"password"`
	SSHPort      int    `yaml:"ssh_port"` // 可选：覆盖全局 Port
	IsMaster     bool   `yaml:"is_master"`
	InstallTools bool   `yaml:"install_tools"`
}

type RegistryConfig struct {
	Endpoint string `yaml:"endpoint"`
	IP       string `yaml:"ip"`
	Port     int    `yaml:"port"`
}

type VersionConfig struct {
	Containerd string `yaml:"containerd"`
	Runc       string `yaml:"runc"`
	Nerdctl    string `yaml:"nerdctl"`
	K8s        string `yaml:"k8s"`
}
