BINARY := cs
INSTALL_DIR := $(shell go env GOPATH)/bin

.PHONY: build install uninstall

build:
	go build -o $(BINARY) .

install: build
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)

uninstall:
	rm -f $(INSTALL_DIR)/$(BINARY)
