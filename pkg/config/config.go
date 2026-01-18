package config

type Config struct {
	// 全局配置
	Registry    RegistryConfig `yaml:"registry"`
	Versions    VersionConfig  `yaml:"versions"`
	JoinCommand string         `yaml:"join_command"` // 供 Worker 节点使用的全局 Join 命令

	// 默认 SSH 配置 (如果 Node 中未指定则使用此默认值)
	SSHPort int    `yaml:"ssh_port"`
	User    string `yaml:"user"`

	// 节点列表
	Nodes          []NodeConfig `yaml:"nodes"`
	ConcurrentExec bool         `yaml:"concurrent_exec"`
}

type NodeConfig struct {
	IP           string `yaml:"ip"`
	Password     string `yaml:"password"`
	User         string `yaml:"user"`     // 可选：覆盖全局 User
	SSHPort      int    `yaml:"ssh_port"` // 可选：覆盖全局 Port
	RemoteTmpDir string `yaml:"remote_tmp_dir"`
	IsMaster     bool   `yaml:"is_master"`
	InstallTools bool   `yaml:"install_tools"`
}

type RegistryConfig struct {
	Endpoint string `yaml:"endpoint"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	UseHTTP  bool   `yaml:"use_http"`
}

type VersionConfig struct {
	Containerd string `yaml:"containerd"`
	Runc       string `yaml:"runc"`
	Nerdctl    string `yaml:"nerdctl"`
	K8s        string `yaml:"k8s"`
}
