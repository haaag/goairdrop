# goairdrop - simple program to receive text 
# See LICENSE file for copyright and license details.

PROJECT_NAME	:= goairdrop
MAIN_SRC	:= main.go
BIN_DIR		:= bin
BIN_PATH	:= $(BIN_DIR)/$(PROJECT_NAME)
LDFLAGS		:= -s -w

# Target to build everything
all: test build

# Build the binary
build:
	@echo '>> Building $(PROJECT_NAME)'
	@CGO_ENABLED=1 go build -ldflags='$(LDFLAGS)' -o $(BIN_PATH) $(MAIN_SRC)
	@echo '>> Binary built at $(BIN_PATH)'

# Run tests
test:
	@echo '>> Testing $(PROJECT_NAME)'
	@go test ./...
	@echo

# Clean binary directories
clean:
	@echo '>> Cleaning bin'
	rm -rf $(BIN_DIR)


.PHONY: all test clean
