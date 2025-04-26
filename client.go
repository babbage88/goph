// Copyright 2020 Mohammed El Bahja. All rights reserved.
// Use of this source code is governed by a MIT license.

package goph

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Client represents Goph client.
type Client struct {
	*ssh.Client
	Config *Config
}

// Config for Client.
type Config struct {
	Auth           Auth
	User           string
	Addr           string
	Port           uint
	Timeout        time.Duration
	Callback       ssh.HostKeyCallback
	BannerCallback ssh.BannerCallback
}

// DefaultTimeout is the timeout of ssh client connection.
var DefaultTimeout = 20 * time.Second

// New starts a new ssh connection, the host public key must be in known hosts.
func New(user string, addr string, auth Auth) (c *Client, err error) {

	callback, err := DefaultKnownHosts()

	if err != nil {
		return
	}

	c, err = NewConn(&Config{
		User:     user,
		Addr:     addr,
		Port:     22,
		Auth:     auth,
		Timeout:  DefaultTimeout,
		Callback: callback,
	})
	return
}

// NewUnknown starts a ssh connection get client without cheking knownhosts.
// PLEASE AVOID USING THIS, UNLESS YOU KNOW WHAT ARE YOU DOING!
// if there a "man in the middle proxy", this can harm you!
// You can add the key to know hosts and use New() func instead!
func NewUnknown(user string, addr string, auth Auth) (*Client, error) {
	return NewConn(&Config{
		User:     user,
		Addr:     addr,
		Port:     22,
		Auth:     auth,
		Timeout:  DefaultTimeout,
		Callback: ssh.InsecureIgnoreHostKey(),
	})
}

// NewConn returns new client and error if any.
func NewConn(config *Config) (c *Client, err error) {

	c = &Client{
		Config: config,
	}

	c.Client, err = Dial("tcp", config)
	return
}

// Dial starts a client connection to SSH server based on config.
func Dial(proto string, c *Config) (*ssh.Client, error) {
	return ssh.Dial(proto, net.JoinHostPort(c.Addr, fmt.Sprint(c.Port)), &ssh.ClientConfig{
		User:            c.User,
		Auth:            c.Auth,
		Timeout:         c.Timeout,
		HostKeyCallback: c.Callback,
		BannerCallback:  c.BannerCallback,
	})
}

// Run starts a new SSH session and runs the cmd, it returns CombinedOutput and err if any.
func (c Client) Run(cmd string) ([]byte, error) {

	var (
		err  error
		sess *ssh.Session
	)

	if sess, err = c.NewSession(); err != nil {
		return nil, err
	}

	defer sess.Close()

	return sess.CombinedOutput(cmd)
}

// Run starts a new SSH session with context and runs the cmd. It returns CombinedOutput and err if any.
func (c Client) RunContext(ctx context.Context, name string) ([]byte, error) {
	cmd, err := c.CommandContext(ctx, name)
	if err != nil {
		return nil, err
	}

	return cmd.CombinedOutput()
}

// Command returns new Cmd and error if any.
func (c Client) Command(name string, args ...string) (*Cmd, error) {

	var (
		sess *ssh.Session
		err  error
	)

	if sess, err = c.NewSession(); err != nil {
		return nil, err
	}

	return &Cmd{
		Path:    name,
		Args:    args,
		Session: sess,
		Context: context.Background(),
	}, nil
}

// Command returns new Cmd with context and error, if any.
func (c Client) CommandContext(ctx context.Context, name string, args ...string) (*Cmd, error) {
	cmd, err := c.Command(name, args...)
	if err != nil {
		return cmd, err
	}

	cmd.Context = ctx

	return cmd, nil
}

// NewSftp returns new sftp client and error if any.
func (c Client) NewSftp(opts ...sftp.ClientOption) (*sftp.Client, error) {
	return sftp.NewClient(c.Client, opts...)
}

// Close client net connection.
func (c Client) Close() error {
	return c.Client.Close()
}

func (c *Client) Upload(srcPath, dstPath string) error {
	stat, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("failed to stat source path: %w", err)
	}

	if stat.IsDir() {
		// Directory upload
		return c.uploadDirectory(srcPath, dstPath)
	}

	// File upload
	return c.uploadFile(srcPath, dstPath)
}

func (c *Client) uploadFile(srcPath, dstPath string) error {
	sftpClient, err := sftp.NewClient(c.Client)
	if err != nil {
		return fmt.Errorf("failed to create sftp client: %w", err)
	}
	defer sftpClient.Close()

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := sftpClient.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create remote file: %w", err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func (c *Client) uploadDirectory(srcDir, dstDir string) error {
	sftpClient, err := sftp.NewClient(c.Client)
	if err != nil {
		return fmt.Errorf("failed to create sftp client: %w", err)
	}
	defer sftpClient.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dstDir, relPath)

		if info.IsDir() {
			sftpClient.MkdirAll(targetPath)
		} else {
			srcFile, err := os.Open(path)
			if err != nil {
				return err
			}
			defer srcFile.Close()

			dstFile, err := sftpClient.Create(targetPath)
			if err != nil {
				return err
			}
			defer dstFile.Close()

			_, err = io.Copy(dstFile, srcFile)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// The original Upload method on the goph package that doesn't handle directories.
func (c Client) UploadV1(localPath string, remotePath string) (err error) {

	local, err := os.Open(localPath)
	if err != nil {
		return
	}
	defer local.Close()

	ftp, err := c.NewSftp()
	if err != nil {
		return
	}
	defer ftp.Close()

	remote, err := ftp.Create(remotePath)
	if err != nil {
		return
	}
	defer remote.Close()

	_, err = io.Copy(remote, local)
	return
}

// The original goph Download method
func (c Client) DownloadV1(remotePath string, localPath string) (err error) {

	local, err := os.Create(localPath)
	if err != nil {
		return
	}
	defer local.Close()

	ftp, err := c.NewSftp()
	if err != nil {
		return
	}
	defer ftp.Close()

	remote, err := ftp.Open(remotePath)
	if err != nil {
		return
	}
	defer remote.Close()

	if _, err = io.Copy(local, remote); err != nil {
		return
	}

	return local.Sync()
}

// Download downloads a file or directory from the remote server to the local filesystem.
func (c Client) Download(remotePath string, localPath string) (err error) {
	sftpClient, err := c.NewSftp()
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	info, err := sftpClient.Stat(remotePath)
	if err != nil {
		return fmt.Errorf("failed to stat remote path: %w", err)
	}

	if info.IsDir() {
		return downloadDirectory(sftpClient, remotePath, localPath)
	}
	return downloadFile(sftpClient, remotePath, localPath)
}

// downloadFile downloads a single file from the remote server.
func downloadFile(sftpClient *sftp.Client, remotePath, localPath string) error {
	srcFile, err := sftpClient.Open(remotePath)
	if err != nil {
		return fmt.Errorf("failed to open remote file: %w", err)
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create local directories: %w", err)
	}

	dstFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy data: %w", err)
	}

	return dstFile.Sync()
}

// downloadDirectory recursively downloads a directory from the remote server.
func downloadDirectory(sftpClient *sftp.Client, remoteDir, localDir string) error {
	walker := sftpClient.Walk(remoteDir)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return err
		}

		relPath, err := filepath.Rel(remoteDir, walker.Path())
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		localPath := filepath.Join(localDir, relPath)

		if walker.Stat().IsDir() {
			if err := os.MkdirAll(localPath, 0755); err != nil {
				return fmt.Errorf("failed to create local directory: %w", err)
			}
			continue
		}

		if err := downloadFile(sftpClient, walker.Path(), localPath); err != nil {
			return err
		}
	}
	return nil
}
