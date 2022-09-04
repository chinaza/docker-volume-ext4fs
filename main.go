package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
)

const socketAddress = "/run/docker/plugins/ext4fs.sock"

type extfsVolume struct {
	Name       string
	Size       string
	MountPoint string

	connections int
}

type extfsDriver struct {
	sync.RWMutex

	root      string
	statePath string
	volumes   map[string]*extfsVolume
}

func newExtfsDriver(root string) (*extfsDriver, error) {
	logrus.WithField("method", "new driver").Debug(root)

	d := &extfsDriver{
		root:      root,
		statePath: filepath.Join(root, "state", "extfs-state.json"),
		volumes:   map[string]*extfsVolume{},
	}

	data, err := os.ReadFile(d.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			logrus.WithField("statePath", d.statePath).Debug("no state found")
		} else {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(data, &d.volumes); err != nil {
			return nil, err
		}
	}

	// * validate filesystem Directory
	fsPath := filepath.Join(d.root, "fs")
	fileInfo, err := os.Lstat(fsPath)
	if err != nil {
		return nil, logError(err.Error())
	}
	if fileInfo != nil && !fileInfo.IsDir() {
		return nil, logError("A file exists at supposed path of volume filesystem dir")
	}

	return d, nil
}

func (d *extfsDriver) saveState() {
	data, err := json.Marshal(d.volumes)
	if err != nil {
		logrus.WithField("statePath", d.statePath).Error(err)
		return
	}

	if err := os.WriteFile(d.statePath, data, 0644); err != nil {
		logrus.WithField("savestate", d.statePath).Error(err)
	}
}

func (d *extfsDriver) Create(r *volume.CreateRequest) error {
	logrus.WithField("method", "create").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v := &extfsVolume{}
	v.Name = r.Name
	v.MountPoint = filepath.Join(d.root, "volumes", r.Name)
	size, ok := r.Options["size"]
	if ok {
		v.Size = size
	} else {
		v.Size = "10G"
	}

	fsPath := filepath.Join(d.root, "fs", v.Name)

	// * Create filesystem file
	fsFileCreate := exec.Command("touch", fsPath)
	fsCreateOut, fsCreateErr := fsFileCreate.CombinedOutput()
	if fsCreateErr != nil {
		return logError(fsCreateErr.Error())
	}
	logrus.Info(fsCreateOut)

	// * Truncate FS to SIZE
	fsTrunc := exec.Command("truncate", "-s", v.Size, fsPath)
	fsTruncOut, fsTruncErr := fsTrunc.CombinedOutput()
	if fsTruncErr != nil {
		return logError(fsCreateErr.Error())
	}
	logrus.Info(fsTruncOut)

	// * Make ext4 FS
	fsMake := exec.Command("mke2fs", "-t", "ext4", "-F", fsPath)
	fsMakeOut, fsMakeErr := fsMake.CombinedOutput()
	if fsMakeErr != nil {
		return logError(fsMakeErr.Error())
	}
	logrus.Info(fsMakeOut)

	d.volumes[v.Name] = v

	d.saveState()

	return nil
}

func (d *extfsDriver) Remove(r *volume.RemoveRequest) error {
	logrus.WithField("method", "remove").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return logError("volume %s not found", r.Name)
	}

	if v.connections != 0 {
		return logError("volume %s is currently used by a container", r.Name)
	}
	if err := os.RemoveAll(v.MountPoint); err != nil {
		return logError("Failed to remove mountpoint. Error: %s", err.Error())
	}
	fsPath := filepath.Join(d.root, "fs", v.Name)
	if err := os.RemoveAll(fsPath); err != nil {
		return logError("Failed to remove ext4fs. Error: %s", err.Error())
	}
	delete(d.volumes, v.Name)
	d.saveState()
	return nil
}

func (d *extfsDriver) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	logrus.WithField("method", "path").Debugf("%#v", r)

	d.RLock()
	defer d.RUnlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.PathResponse{}, logError("volume %s not found", r.Name)
	}

	return &volume.PathResponse{Mountpoint: v.MountPoint}, nil
}

func (d *extfsDriver) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	logrus.WithField("method", "mount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.MountResponse{}, logError("volume %s not found", r.Name)
	}

	if v.connections == 0 {
		fi, err := os.Lstat(v.MountPoint)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(v.MountPoint, 0755); err != nil {
				return &volume.MountResponse{}, logError(err.Error())
			}
		} else if err != nil {
			return &volume.MountResponse{}, logError(err.Error())
		}

		if fi != nil && !fi.IsDir() {
			return &volume.MountResponse{}, logError("%v already exist and it's not a directory", v.MountPoint)
		}

		if err := d.mountVolume(v); err != nil {
			return &volume.MountResponse{}, logError(err.Error())
		}
	}

	v.connections++

	return &volume.MountResponse{Mountpoint: v.MountPoint}, nil
}

func (d *extfsDriver) Unmount(r *volume.UnmountRequest) error {
	logrus.WithField("method", "unmount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v, ok := d.volumes[r.Name]
	if !ok {
		return logError("volume %s not found", r.Name)
	}

	v.connections--

	if v.connections <= 0 {
		if err := d.unmountVolume(v.MountPoint); err != nil {
			return logError(err.Error())
		}
		v.connections = 0
	}

	return nil
}

func (d *extfsDriver) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	logrus.WithField("method", "get").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.GetResponse{}, logError("volume %s not found", r.Name)
	}

	return &volume.GetResponse{Volume: &volume.Volume{Name: v.Name, Mountpoint: v.MountPoint}}, nil
}

func (d *extfsDriver) List() (*volume.ListResponse, error) {
	logrus.WithField("method", "list").Debugf("")

	d.Lock()
	defer d.Unlock()

	var vols []*volume.Volume
	for name, v := range d.volumes {
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: v.MountPoint})
	}
	return &volume.ListResponse{Volumes: vols}, nil
}

func (d *extfsDriver) Capabilities() *volume.CapabilitiesResponse {
	logrus.WithField("method", "capabilities").Debugf("")

	return &volume.CapabilitiesResponse{Capabilities: volume.Capability{Scope: "local"}}
}

func (d *extfsDriver) mountVolume(v *extfsVolume) error {
	fsPath := filepath.Join(d.root, "fs", v.Name)

	cmd := exec.Command("mount", fsPath, v.MountPoint)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return logError("Mount command execute failed: %v (%s)", err, output)
	}
	return nil
}

func (d *extfsDriver) unmountVolume(target string) error {
	cmd := fmt.Sprintf("umount %s", target)
	logrus.Debug(cmd)
	return exec.Command("sh", "-c", cmd).Run()
}

func logError(format string, args ...interface{}) error {
	logrus.Errorf(format, args...)
	return fmt.Errorf(format, args...)
}

func main() {
	logrus.SetLevel(logrus.DebugLevel)

	d, err := newExtfsDriver("/mnt")
	if err != nil {
		log.Fatal(err)
	}
	h := volume.NewHandler(d)
	logrus.Infof("listening on %s", socketAddress)
	logrus.Info(h.ServeUnix(socketAddress, 0))
}
