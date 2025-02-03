package helm

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	klog "k8s.io/klog/v2"
)

var CommandPath = "./helm"

// UpgradeOptions holds all the options for upgrading / installing a chart
type UpgradeOptions struct {
	Chart string
	Path  string

	Repo            string
	Version         string
	Values          string
	SetValues       map[string]string
	SetStringValues map[string]string

	Username string
	Password string

	Atomic          bool
	Force           bool
	CreateNamespace bool

	InsecureSkipTLSVerify bool

	ExtraArgs []string
}

// Client defines the interface how to interact with helm
type Client interface {
	Install(name, namespace string, options UpgradeOptions) error
	Upgrade(name, namespace string, options UpgradeOptions) error
	Rollback(name, namespace string, revision string) error
	Delete(name, namespace string) error
	Exists(name, namespace string) (bool, error)
}

type client struct {
	config   *clientcmdapi.Config
	helmPath string

	stderr io.Writer
	stdout io.Writer
}

// NewClient creates a new helm client from the given config
func NewClient(config *clientcmdapi.Config) Client {
	return &client{
		config:   config,
		helmPath: CommandPath,
	}
}

// NewClientWithStreams creates a new helm client from the given config
func NewClientWithStreams(helmPath string, config *clientcmdapi.Config, stdout, stderr io.Writer) Client {
	return &client{
		config:   config,
		helmPath: helmPath,

		stderr: stderr,
		stdout: stdout,
	}
}

func (c *client) exec(args []string) error {
	if len(args) == 0 {
		return nil
	}

	cmd := exec.Command(c.helmPath, args...)
	if c.stdout != nil {
		cmd.Stdout = c.stdout
		cmd.Stderr = c.stderr
		return cmd.Run()
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "release: not found") {
			return nil
		}
		klog.TODO().Error(
			err,
			"error executing helm",
			"args", args,
			"output", string(output),
		)
		return fmt.Errorf("error executing helm %s: %s", args[0], string(output))
	}

	return nil
}

func (c *client) Rollback(name, namespace string, revision string) error {
	kubeConfig, err := WriteKubeConfig(c.config)
	if err != nil {
		return err
	}
	defer os.Remove(kubeConfig)

	args := []string{"rollback", name}
	if revision != "" {
		args = append(args, revision)
	}
	args = append(args, "--namespace", namespace, "--kubeconfig", kubeConfig)
	return c.exec(args)
}

func (c *client) Install(name, namespace string, options UpgradeOptions) error {
	return c.run(name, namespace, options, "install", options.ExtraArgs)
}

func (c *client) Upgrade(name, namespace string, options UpgradeOptions) error {
	options.ExtraArgs = append(options.ExtraArgs, "--install")
	return c.run(name, namespace, options, "upgrade", options.ExtraArgs)
}

func (c *client) run(name, namespace string, options UpgradeOptions, command string, extraArgs []string) error {
	kubeConfig, err := WriteKubeConfig(c.config)
	if err != nil {
		return err
	}
	defer os.Remove(kubeConfig)

	args := []string{command, name}
	if options.Path != "" {
		args = append(args, options.Path)
	} else if options.Chart != "" {
		args = append(args, options.Chart)

		if options.Repo == "" {
			return fmt.Errorf("chart repo cannot be null")
		}

		args = append(args, "--repo", options.Repo)
		if options.Version != "" {
			args = append(args, "--version", options.Version)
		}
		if options.Username != "" {
			args = append(args, "--username", options.Username)
		}
		if options.Password != "" {
			args = append(args, "--password", options.Password)
		}
	}

	args = append(args, "--kubeconfig", kubeConfig, "--namespace", namespace)
	args = append(args, extraArgs...)
	if options.CreateNamespace {
		args = append(args, "--create-namespace")
	}

	// Values
	if options.Values != "" {
		// Create temp file
		tempFile, err := os.CreateTemp("", "")
		if err != nil {
			return errors.Wrap(err, "create temp file")
		}

		// Write to temp file
		_, err = tempFile.Write([]byte(options.Values))
		if err != nil {
			os.Remove(tempFile.Name())
			return errors.Wrap(err, "write temp file")
		}

		// Close temp file
		tempFile.Close()
		defer os.Remove(tempFile.Name())

		// Wait quickly so helm will find the file
		time.Sleep(time.Millisecond)

		args = append(args, "--values", tempFile.Name())
	}

	// Set values
	if len(options.SetValues) > 0 {
		args = append(args, "--set")

		setString := ""
		for key, value := range options.SetValues {
			if setString != "" {
				setString += ","
			}

			setString += key + "=" + value
		}

		args = append(args, setString)
	}

	// Set string values
	if len(options.SetStringValues) > 0 {
		args = append(args, "--set-string")

		setString := ""
		for key, value := range options.SetStringValues {
			if setString != "" {
				setString += ","
			}

			setString += key + "=" + value
		}

		args = append(args, setString)
	}

	if options.Force {
		args = append(args, "--force")
	}
	if options.Atomic {
		args = append(args, "--atomic")
	}
	if options.InsecureSkipTLSVerify {
		args = append(args, "--insecure-skip-tls-verify")
	}

	return c.exec(args)
}

func (c *client) Delete(name, namespace string) error {
	kubeConfig, err := WriteKubeConfig(c.config)
	if err != nil {
		return err
	}
	defer os.Remove(kubeConfig)

	args := []string{"delete", name, "--namespace", namespace, "--kubeconfig", kubeConfig}
	return c.exec(args)
}

func (c *client) Exists(name, namespace string) (bool, error) {
	kubeConfig, err := WriteKubeConfig(c.config)
	if err != nil {
		return false, err
	}
	defer os.Remove(kubeConfig)

	args := []string{"status", name, "--namespace", namespace, "--kubeconfig", kubeConfig}
	output, err := exec.Command(c.helmPath, args...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "release: not found") {
			return false, nil
		}

		return false, fmt.Errorf("error executing helm status: %s", string(output))
	}

	return true, nil
}

// WriteKubeConfig writes the kubeconfig to a file and returns the filename
func WriteKubeConfig(configRaw *clientcmdapi.Config) (string, error) {
	data, err := clientcmd.Write(*configRaw)
	if err != nil {
		return "", err
	}

	// Create temp file
	tempFile, err := os.CreateTemp("", "")
	if err != nil {
		return "", errors.Wrap(err, "create temp file")
	}

	// Write to temp file
	_, err = tempFile.Write(data)
	if err != nil {
		os.Remove(tempFile.Name())
		return "", errors.Wrap(err, "write temp file")
	}

	// Close temp file
	tempFile.Close()

	// Okay sometimes the file is written so quickly that helm somehow
	// cannot read it immediately which causes errors
	// so we wait here till the file is ready
	now := time.Now()
	for time.Since(now) < time.Minute {
		_, err = os.Stat(tempFile.Name())
		if err != nil {
			if os.IsNotExist(err) {
				time.Sleep(time.Millisecond * 50)
				continue
			}

			os.Remove(tempFile.Name())
			return "", err
		}

		break
	}

	return tempFile.Name(), nil
}
