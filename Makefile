PLUGIN_NAME = onlyzhap/ext4fs
PLUGIN_TAG ?= latest

all: clean rootfs create enable

clean:
	@echo "### rm ./plugin"
	@rm -rf ./plugin
	@echo "### remove existing plugin ${PLUGIN_NAME} if exists"
	@docker plugin rm -f ${PLUGIN_NAME} || true

rootfs:
	@echo "### docker build: rootfs image with docker-volume-ext4fs"
	@docker build -q -t ${PLUGIN_NAME}:rootfs .
	@echo "### create rootfs directory in ./plugin/rootfs"
	@mkdir -p ./plugin/rootfs
	@docker create --name tmp ${PLUGIN_NAME}:rootfs
	@docker export tmp | tar -x -C ./plugin/rootfs
	@echo "### copy config.json to ./plugin/"
	@cp config.json ./plugin/
	@docker rm -vf tmp

create:
	@echo "### create new plugin ${PLUGIN_NAME} from ./plugin"
	@docker plugin create ${PLUGIN_NAME} ./plugin

enable:		
	@echo "### enable plugin ${PLUGIN_NAME}"		
	@docker plugin enable ${PLUGIN_NAME}

push:  clean rootfs create enable
	@echo "### push plugin ${PLUGIN_NAME}"
	@docker plugin push ${PLUGIN_NAME}