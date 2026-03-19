// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package admissionwebhook

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

// kubeConfig is a minimal representation of a kubeconfig file.
type kubeConfig struct {
	Clusters       []kubeConfigCluster `yaml:"clusters"`
	Users          []kubeConfigUser    `yaml:"users"`
	Contexts       []kubeConfigContext `yaml:"contexts"`
	CurrentContext string              `yaml:"current-context"`
}

type kubeConfigCluster struct {
	Name    string `yaml:"name"`
	Cluster struct {
		Server                   string `yaml:"server"`
		CertificateAuthority     string `yaml:"certificate-authority"`
		CertificateAuthorityData string `yaml:"certificate-authority-data"`
		InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
	} `yaml:"cluster"`
}

type kubeConfigUser struct {
	Name string `yaml:"name"`
	User struct {
		Token                 string `yaml:"token"`
		ClientCertificate     string `yaml:"client-certificate"`
		ClientCertificateData string `yaml:"client-certificate-data"`
		ClientKey             string `yaml:"client-key"`
		ClientKeyData         string `yaml:"client-key-data"`
	} `yaml:"user"`
}

type kubeConfigContext struct {
	Name    string `yaml:"name"`
	Context struct {
		Cluster string `yaml:"cluster"`
		User    string `yaml:"user"`
	} `yaml:"context"`
}

// newK8sDiscoveryClientFromKubeconfig creates a K8sDiscoveryClient from the default kubeconfig.
func newK8sDiscoveryClientFromKubeconfig() (*K8sDiscoveryClient, error) {
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, errors.New("not running in-cluster and cannot determine home directory for kubeconfig")
		}
		kubeconfigPath = filepath.Join(home, ".kube", "config")
	}

	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil, errors.Wrapf(err, "not running in-cluster and failed to read kubeconfig from %s", kubeconfigPath)
	}

	var cfg kubeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, errors.Wrap(err, "failed to parse kubeconfig")
	}

	if cfg.CurrentContext == "" {
		return nil, errors.New("kubeconfig has no current-context")
	}

	// Find the current context.
	var clusterName, userName string
	for _, ctx := range cfg.Contexts {
		if ctx.Name == cfg.CurrentContext {
			clusterName = ctx.Context.Cluster
			userName = ctx.Context.User
			break
		}
	}
	if clusterName == "" {
		return nil, fmt.Errorf("kubeconfig context %q not found", cfg.CurrentContext)
	}

	// Find the cluster.
	var server string
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	for _, c := range cfg.Clusters {
		if c.Name == clusterName {
			server = c.Cluster.Server
			if c.Cluster.InsecureSkipTLSVerify {
				tlsConfig.InsecureSkipVerify = true //nolint:gosec // user kubeconfig setting
			}
			if c.Cluster.CertificateAuthorityData != "" {
				caData, err := base64.StdEncoding.DecodeString(c.Cluster.CertificateAuthorityData)
				if err != nil {
					return nil, errors.Wrap(err, "failed to decode certificate-authority-data")
				}
				pool := x509.NewCertPool()
				if !pool.AppendCertsFromPEM(caData) {
					return nil, errors.New("failed to parse certificate-authority-data")
				}
				tlsConfig.RootCAs = pool
			} else if c.Cluster.CertificateAuthority != "" {
				caData, err := os.ReadFile(c.Cluster.CertificateAuthority)
				if err != nil {
					return nil, errors.Wrapf(err, "failed to read certificate-authority file %s", c.Cluster.CertificateAuthority)
				}
				pool := x509.NewCertPool()
				if !pool.AppendCertsFromPEM(caData) {
					return nil, errors.New("failed to parse certificate-authority file")
				}
				tlsConfig.RootCAs = pool
			}
			break
		}
	}
	if server == "" {
		return nil, fmt.Errorf("kubeconfig cluster %q not found", clusterName)
	}

	// Find the user.
	var token string
	for _, u := range cfg.Users {
		if u.Name == userName {
			token = u.User.Token
			// Support client certificate auth.
			if u.User.ClientCertificateData != "" && u.User.ClientKeyData != "" {
				certData, err := base64.StdEncoding.DecodeString(u.User.ClientCertificateData)
				if err != nil {
					return nil, errors.Wrap(err, "failed to decode client-certificate-data")
				}
				keyData, err := base64.StdEncoding.DecodeString(u.User.ClientKeyData)
				if err != nil {
					return nil, errors.Wrap(err, "failed to decode client-key-data")
				}
				cert, err := tls.X509KeyPair(certData, keyData)
				if err != nil {
					return nil, errors.Wrap(err, "failed to load client certificate")
				}
				tlsConfig.Certificates = []tls.Certificate{cert}
			} else if u.User.ClientCertificate != "" && u.User.ClientKey != "" {
				cert, err := tls.LoadX509KeyPair(u.User.ClientCertificate, u.User.ClientKey)
				if err != nil {
					return nil, errors.Wrap(err, "failed to load client certificate files")
				}
				tlsConfig.Certificates = []tls.Certificate{cert}
			}
			break
		}
	}

	return &K8sDiscoveryClient{
		httpClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		},
		apiHost: server,
		token:   token,
	}, nil
}
