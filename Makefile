NAME := ovs-flowmon
DIST_DIR ?= build
OUTPUT := $(DIST_DIR)/$(NAME)
DOCKER := $(shell which podman)
ifeq ($(DOCKER),)
	DOCKER := $(shell which docker)
endif

.PHONY: all
all: build

.PHONY: prepare
prepare:
	@mkdir -p $(DIST_DIR)

.PHONY: build
build: prepare
	@go build -o $(OUTPUT) cmd/ovs-flowmon/main.go

.PHONY: image
image:
	$(DOCKER) build -t ovs-flowmon .

.PHONY: clean
clean:
	@rm -rf $(DIST_DIR)


