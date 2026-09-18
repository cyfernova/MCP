.DEFAULT_GOAL := help

GO ?= go
OPENSSL ?= openssl

APP_NAME := mcp
GEN_SWAGGER_NAME := gen-swagger
BIN_DIR := bin
CERT_DIR := certs
SWAGGER_JSON := swagger.json
SWAGGER_ASSET := cmd/swagger-assets/swagger.json

ifeq ($(OS),Windows_NT)
EXE_EXT := .exe
BIN_DIR_NATIVE := $(subst /,\,$(BIN_DIR))
CERT_DIR_NATIVE := $(subst /,\,$(CERT_DIR))
SWAGGER_ASSET_NATIVE := $(subst /,\,$(SWAGGER_ASSET))
MKDIR_BIN = if not exist "$(BIN_DIR_NATIVE)" mkdir "$(BIN_DIR_NATIVE)"
MKDIR_CERTS = if not exist "$(CERT_DIR_NATIVE)" mkdir "$(CERT_DIR_NATIVE)"
REMOVE_BIN = if exist "$(BIN_DIR_NATIVE)" rmdir /S /Q "$(BIN_DIR_NATIVE)"
COPY_SWAGGER = copy /Y "$(SWAGGER_JSON)" "$(SWAGGER_ASSET_NATIVE)" >NUL
REMOVE_CERT_TEMP = del /Q "$(CERT_DIR_NATIVE)\server.csr" "$(CERT_DIR_NATIVE)\client.csr" "$(CERT_DIR_NATIVE)\ca.srl"
RUN_PREFIX :=
else
EXE_EXT :=
MKDIR_BIN = mkdir -p "$(BIN_DIR)"
MKDIR_CERTS = mkdir -p "$(CERT_DIR)"
REMOVE_BIN = rm -rf "$(BIN_DIR)"
COPY_SWAGGER = cp "$(SWAGGER_JSON)" "$(SWAGGER_ASSET)"
REMOVE_CERT_TEMP = rm -f "$(CERT_DIR)/server.csr" "$(CERT_DIR)/client.csr" "$(CERT_DIR)/ca.srl"
RUN_PREFIX := ./
endif

MCP_BIN := $(BIN_DIR)/$(APP_NAME)$(EXE_EXT)
GEN_SWAGGER_BIN := $(BIN_DIR)/$(GEN_SWAGGER_NAME)$(EXE_EXT)

ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: help build run clean test certs swagger

help:
	@echo MCP Server Setup
	@echo Available targets:
	@echo "  make build   - Build the MCP server binary"
	@echo "  make run     - Run the MCP server; loads .env when present"
	@echo "  make test    - Run tests"
	@echo "  make clean   - Clean build artifacts"
	@echo "  make certs   - Generate self-signed TLS certificates for mTLS"
	@echo "  make swagger - Generate swagger.json from tool catalog"

build:
	$(MKDIR_BIN)
	$(GO) build -o "$(MCP_BIN)" ./cmd/mcp

run: build
	$(RUN_PREFIX)$(MCP_BIN)

test:
	$(GO) test -v ./...

clean:
	$(REMOVE_BIN)

certs:
	$(MKDIR_CERTS)
	@echo Generating CA certificate...
	$(OPENSSL) req -x509 -newkey rsa:4096 -keyout "$(CERT_DIR)/ca.key" -out "$(CERT_DIR)/ca.crt" -days 365 -nodes -subj "/CN=MCP Dev CA"
	@echo Generating server certificate...
	$(OPENSSL) req -newkey rsa:4096 -keyout "$(CERT_DIR)/server.key" -out "$(CERT_DIR)/server.csr" -nodes -subj "/CN=localhost"
	$(OPENSSL) x509 -req -in "$(CERT_DIR)/server.csr" -CA "$(CERT_DIR)/ca.crt" -CAkey "$(CERT_DIR)/ca.key" -CAcreateserial -out "$(CERT_DIR)/server.crt" -days 365
	@echo Generating client certificate...
	$(OPENSSL) req -newkey rsa:4096 -keyout "$(CERT_DIR)/client.key" -out "$(CERT_DIR)/client.csr" -nodes -subj "/CN=MCP Client"
	$(OPENSSL) x509 -req -in "$(CERT_DIR)/client.csr" -CA "$(CERT_DIR)/ca.crt" -CAkey "$(CERT_DIR)/ca.key" -CAcreateserial -out "$(CERT_DIR)/client.crt" -days 365
	$(REMOVE_CERT_TEMP)
	@echo Certificates generated successfully.
	@echo "  CA: $(CERT_DIR)/ca.crt"
	@echo "  Server: $(CERT_DIR)/server.crt, $(CERT_DIR)/server.key"
	@echo "  Client: $(CERT_DIR)/client.crt, $(CERT_DIR)/client.key"

swagger:
	$(MKDIR_BIN)
	$(GO) build -o "$(GEN_SWAGGER_BIN)" ./cmd/gen-swagger
	$(RUN_PREFIX)$(GEN_SWAGGER_BIN) "$(SWAGGER_JSON)"
	$(COPY_SWAGGER)
