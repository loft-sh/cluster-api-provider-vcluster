package kubeconfighelper

import (
	"fmt"

	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func NewLocalConfigFor(config *rest.Config, userInfo user.Info) (*clientcmdapi.Config, error) {
	config = rest.CopyConfig(config)
	config.Impersonate = rest.ImpersonationConfig{
		UserName: userInfo.GetName(),
		Groups:   userInfo.GetGroups(),
		Extra:    userInfo.GetExtra(),
	}

	return ConvertRestConfigToRawConfig(config)
}

func ConvertRestConfigToRawConfig(config *rest.Config) (*clientcmdapi.Config, error) {
	raw, err := ConvertRestConfigToClientConfig(config).RawConfig()
	return &raw, err
}

func ConvertRestConfigToClientConfig(config *rest.Config) clientcmd.ClientConfig {
	contextName := "local"
	kubeConfig := clientcmdapi.NewConfig()
	kubeConfig.Contexts = map[string]*clientcmdapi.Context{
		contextName: {
			Cluster:  contextName,
			AuthInfo: contextName,
		},
	}
	kubeConfig.Clusters = map[string]*clientcmdapi.Cluster{
		contextName: {
			Server:                   config.Host,
			InsecureSkipTLSVerify:    config.Insecure,
			CertificateAuthorityData: config.CAData,
			CertificateAuthority:     config.CAFile,
		},
	}
	kubeConfig.AuthInfos = map[string]*clientcmdapi.AuthInfo{
		contextName: {
			Token:                 config.BearerToken,
			TokenFile:             config.BearerTokenFile,
			Impersonate:           config.Impersonate.UserName,
			ImpersonateGroups:     config.Impersonate.Groups,
			ImpersonateUserExtra:  config.Impersonate.Extra,
			ClientCertificate:     config.CertFile,
			ClientCertificateData: config.CertData,
			ClientKey:             config.KeyFile,
			ClientKeyData:         config.KeyData,
			Username:              config.Username,
			Password:              config.Password,
			AuthProvider:          config.AuthProvider,
			Exec:                  config.ExecProvider,
		},
	}
	kubeConfig.CurrentContext = contextName
	return clientcmd.NewDefaultClientConfig(*kubeConfig, &clientcmd.ConfigOverrides{})
}

func NewVClusterClientConfig(name, namespace string, token string, clientCert, clientKey []byte) (*rest.Config, error) {
	config := clientcmdapi.NewConfig()
	contextName := "default"
	clusterConfig := clientcmdapi.NewCluster()
	clusterConfig.Server = fmt.Sprintf("https://%s.%s:443", name, namespace)
	clusterConfig.InsecureSkipTLSVerify = true

	authInfo := clientcmdapi.NewAuthInfo()
	authInfo.ClientCertificateData = clientCert
	authInfo.ClientKeyData = clientKey
	authInfo.Token = token

	// Update kube context
	context := clientcmdapi.NewContext()
	context.Cluster = contextName
	context.AuthInfo = contextName

	config.Clusters[contextName] = clusterConfig
	config.AuthInfos[contextName] = authInfo
	config.Contexts[contextName] = context
	config.CurrentContext = contextName

	restConfig, err := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, err
	}

	return restConfig, nil
}
