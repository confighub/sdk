// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WritePrivateKey stores a private key in the key directory and returns where it
// went. name is resolved by KeyPath, so a bare alias becomes <keydir>/<name>.jwk
// and a path is honoured as given.
//
// Refuses to overwrite: the file is frequently the only copy of a key whose
// public half is already registered, so replacing it would revoke a working
// credential irrecoverably. Anything else provisioning a key into this directory
// goes through here, so the permissions and that rule stay the same.
func (s *Store) WritePrivateKey(name string, privateJWK json.RawMessage) (string, error) {
	path := s.KeyPath(name)

	// 0700: the key directory itself should not be listable by other users,
	// since the file names identify which credentials exist.
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%s already exists; remove it or choose another path", path)
		}
		return "", fmt.Errorf("writing private key: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(append(privateJWK, '\n')); err != nil {
		return "", fmt.Errorf("writing private key: %w", err)
	}
	return path, nil
}
