// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package workerapi

import "testing"

// Every AppConfig toolchain needs an extension: render-configmap names the files it renders
// by it, and upload recognizes a bundle's files by it.
func TestAppConfigFileExtensionsCoverEveryAppConfigToolchain(t *testing.T) {
	for toolchain := range AppConfigToolchains {
		if _, ok := AppConfigFileExtensions[toolchain]; !ok {
			t.Errorf("%s has no entry in AppConfigFileExtensions", toolchain)
		}
	}
}

func TestFileExtensionForToolchain(t *testing.T) {
	for toolchain, want := range map[ToolchainType]string{
		ToolchainAppConfigProperties: ".properties",
		ToolchainAppConfigText:       ".txt",
		"AppConfig/Foo":              ".foo",
		"AppConfig/":                 ".config",
	} {
		if got := FileExtensionForToolchain(toolchain); got != want {
			t.Errorf("FileExtensionForToolchain(%q) = %q, want %q", toolchain, got, want)
		}
	}
}
