# Docker volume plugin for Local ext4 Filesystem

This plugin allows you to create and mount linux ext4 filesystem in your container easily.

_Inspired by <https://github.com/vieux/docker-volume-sshfs>_

## Usage

1 - Install the plugin

```sh
docker plugin install onlyzhap/ext4fs
```

2 - Create a volume

```sh
$ docker volume create -d onlyzhap/ext4fs -o size=4G ext4volume
sshvolume
$ docker volume ls
DRIVER                          VOLUME NAME
onlyzhap/ext4fs:latest          ext4volume
```

3 - Use the volume

```sh
docker run -it -v ext4fsvolume:/test busybox ls /test
```
