/*
Copyright (c) Edgeless Systems GmbH

SPDX-License-Identifier: AGPL-3.0-only
*/

package setup

import (
	"io/fs"
	"os"
	"syscall"

	"github.com/edgelesssys/constellation/internal/cloud/metadata"
)

// Mounter is an interface for mount and unmount operations.
type Mounter interface {
	Mount(source string, target string, fstype string, flags uintptr, data string) error
	Unmount(target string, flags int) error
	MkdirAll(path string, perm fs.FileMode) error
}

// DeviceMapper is an interface for device mapping operations.
type DeviceMapper interface {
	DiskUUID() string
	FormatDisk(passphrase string) error
	MapDisk(target string, passphrase string) error
	UnmapDisk(target string) error
}

// ConfigurationGenerator is an interface for generating systemd-cryptsetup@.service unit files.
type ConfigurationGenerator interface {
	Generate(volumeName, encryptedDevice, keyFile, options string) error
}

// MetadataAPI is an interface for accessing cloud metadata.
type MetadataAPI interface {
	metadata.InstanceSelfer
	metadata.InstanceLister
}

// RecoveryDoer is an interface to perform key recovery operations.
// Calls to Do may be blocking, and if successful return a passphrase and measurementSecret.
type RecoveryDoer interface {
	Do(uuid, endpoint string) (passphrase, measurementSecret []byte, err error)
}

// DiskMounter uses the syscall package to mount disks.
type DiskMounter struct{}

// Mount performs a mount syscall.
func (m DiskMounter) Mount(source string, target string, fstype string, flags uintptr, data string) error {
	return syscall.Mount(source, target, fstype, flags, data)
}

// Unmount performs an unmount syscall.
func (m DiskMounter) Unmount(target string, flags int) error {
	return syscall.Unmount(target, flags)
}

// MkdirAll uses os.MkdirAll to create the directory.
func (m DiskMounter) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}
