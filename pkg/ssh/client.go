package ssh

import (
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Client struct {
	client *ssh.Client
	sftp   *sftp.Client
}

func NewClient(ip string, port int, user, password string) (*Client, error) {
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", ip, port)
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %v", err)
	}

	sftpClient, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create sftp client: %v", err)
	}

	return &Client{
		client: conn,
		sftp:   sftpClient,
	}, nil
}

func (c *Client) Close() {
	if c.sftp != nil {
		c.sftp.Close()
	}
	if c.client != nil {
		c.client.Close()
	}
}

// RunCommand 执行远程命令并返回输出 (Stdout + Stderr)
func (c *Client) RunCommand(cmd string) (string, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	outStr := string(output)
	if err != nil {
		return outStr, fmt.Errorf("command '%s' failed: %v, output: %s", cmd, err, strings.TrimSpace(outStr))
	}
	return strings.TrimSpace(outStr), nil
}

// DetectArch 检测远程架构
func (c *Client) DetectArch() (string, error) {
	out, err := c.RunCommand("uname -m")
	if err != nil {
		return "", err
	}
	if strings.Contains(out, "x86_64") {
		return "amd64", nil
	}
	if strings.Contains(out, "aarch64") {
		return "arm64", nil
	}
	return strings.TrimSpace(out), nil
}

// WriteFile 将流数据写入远程文件 (Stream Mode)
// 修复：接收 io.Reader 替代 []byte，解决大文件内存占用和传输阻塞问题
func (c *Client) WriteFile(remotePath string, src io.Reader) error {
	// 1. 强制转换为正斜杠
	remotePath = filepath.ToSlash(remotePath)

	dir := path.Dir(remotePath)

	// 2. 确保父目录存在
	_, err := c.RunCommand(fmt.Sprintf("mkdir -p %s", dir))
	if err != nil {
		return fmt.Errorf("mkdir -p %s failed: %v", dir, err)
	}

	// 3. SFTP 创建文件
	f, err := c.sftp.Create(remotePath)
	if err != nil {
		return fmt.Errorf("sftp create file %s failed: %v", remotePath, err)
	}
	defer f.Close()

	// 4. 流式拷贝 (Buffer Copy)
	// io.Copy 内部会自动处理分块读取和写入，比一次性 Write 稳定得多
	if _, err := io.Copy(f, src); err != nil {
		return fmt.Errorf("sftp transfer failed: %v", err)
	}

	f.Chmod(0755)
	return nil
}

// WriteFile 将内存数据直接写入远程文件
func (c *Client) WriteFile2(remotePath string, data []byte) error {
	// 1. 强制转换为正斜杠 (处理 Windows 上可能的输入: "dir\file")
	remotePath = filepath.ToSlash(remotePath)

	// 2. 使用 path 包获取目录 (path 包始终使用 '/'，而 filepath.Dir 在 Windows 上会返回 '\')
	dir := path.Dir(remotePath)

	// 3. 确保父目录存在 (Linux 命令)
	command, err := c.RunCommand(fmt.Sprintf("mkdir -p %s", dir))
	if err != nil {
		return fmt.Errorf("mkdir -p %s command failed: %v (output: %s)", dir, err, command)
	}

	// 4. SFTP 创建文件
	f, err := c.sftp.Create(remotePath)
	if err != nil {
		return fmt.Errorf("sftp create file %s failed: %v", remotePath, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write file failed: %v", err)
	}

	f.Chmod(0755)
	return nil
}
