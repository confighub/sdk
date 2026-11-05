// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// clusterSecretsDirFlagUsage is the usage of --secrets-dir, on every command
// that puts a ConfigHub credential in the cluster.
const clusterSecretsDirFlagUsage = "write the Secrets holding ConfigHub credentials (argobot's key, the controller's pull token) to this directory as Kubernetes Secret manifests instead of applying them, for a secret store, such as the External Secrets Operator, to put in the cluster"

// clusterSecret is a Secret holding a ConfigHub credential that a cluster
// command puts in the cluster: manifest is the Kubernetes manifest applied for
// it, the Secret with its values in stringData, and for argobot's key the
// Namespace before it.
type clusterSecret struct {
	namespace, name string
	manifest        []byte
}

// clusterPutSecret puts s in the cluster. With secretsDir empty it applies it,
// registering its deletion first, so a partial apply is undone too. Otherwise
// it writes s's Kubernetes manifest, exactly what would have been applied, to a
// file in secretsDir, readable only by the user since the values are in plain
// text, and leaves the cluster alone: an operator whose cluster takes
// credentials only from a secret store loads the manifest's values into the
// store, which keeps the Secret in the cluster. A failed run removes the file,
// since the rollback also revokes the credential in it.
func clusterPutSecret(out io.Writer, kubeconfigPath, secretsDir string, s clusterSecret, registerRollback func(clusterUndo)) error {
	if secretsDir == "" {
		registerRollback(clusterUndoKubectlDeleteSecret(out, kubeconfigPath, s.namespace, s.name))
		fmt.Fprintf(out, "Applying Secret %s/%s...\n", s.namespace, s.name)
		if err := clusterKubectlApply(ctx, kubeconfigPath, s.manifest, out); err != nil {
			return fmt.Errorf("apply Secret %s/%s: %w", s.namespace, s.name, err)
		}
		return nil
	}
	path := clusterSecretFilePath(secretsDir, s)
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return fmt.Errorf("--secrets-dir: %w", err)
	}
	registerRollback(clusterUndo{
		what: "the Secret file " + path,
		fix:  []string{"rm -f " + path},
		run: func() error {
			fmt.Fprintf(out, "Rolling back: removing %s\n", path)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		},
	})
	// WriteFile keeps the mode of a file that exists, which a file written by
	// hand may not have.
	if err := os.WriteFile(path, s.manifest, 0o600); err != nil {
		return fmt.Errorf("write Secret %s/%s: %w", s.namespace, s.name, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("write Secret %s/%s: %w", s.namespace, s.name, err)
	}
	fmt.Fprintf(out, "Wrote the Kubernetes manifest of Secret %s/%s to %s, without applying it. Load it into your secret store:\n"+
		"  the Secret the store keeps in the cluster needs the manifest's type, labels, annotations and keys.\n",
		s.namespace, s.name, path)
	return nil
}

// clusterSecretFilePath is the file in secretsDir that clusterPutSecret writes
// s's manifest to. Neither a namespace nor a Secret name can contain an underscore, so no
// two Secrets share a file.
func clusterSecretFilePath(secretsDir string, s clusterSecret) string {
	return filepath.Join(secretsDir, s.namespace+"_"+s.name+".yaml")
}

// clusterSecretsDirFor is where a cluster's Secrets go for --secrets-dir flag:
// a directory per cluster, since every cluster's Secrets have the same names.
// "" when the flag is not given.
func clusterSecretsDirFor(flag, clusterName string) string {
	if flag == "" {
		return ""
	}
	return filepath.Join(flag, clusterName)
}

// clusterArgobotKeySecret names argobot's key Secret, without its manifest.
func clusterArgobotKeySecret() clusterSecret {
	return clusterSecret{namespace: clusterArgobotNamespace, name: clusterArgobotSecret}
}

// clusterDescribeSecret says where s is, for a command's summary.
func clusterDescribeSecret(secretsDir string, s clusterSecret) string {
	if secretsDir == "" {
		return fmt.Sprintf("Secret %s/%s", s.namespace, s.name)
	}
	return fmt.Sprintf("Secret %s/%s, its manifest written to %s for your secret store", s.namespace, s.name, clusterSecretFilePath(secretsDir, s))
}
