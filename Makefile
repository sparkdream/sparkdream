BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
COMMIT := $(shell git log -1 --format='%H')
APPNAME := sparkdream

# do not override user values
ifeq (,$(VERSION))
  VERSION := $(shell git describe --exact-match 2>/dev/null)
  # if VERSION is empty, then populate it with branch name and raw commit hash
  ifeq (,$(VERSION))
    VERSION := $(BRANCH)-$(COMMIT)
  endif
endif

# Update the ldflags with the app, client & server names
ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=$(APPNAME) \
	-X github.com/cosmos/cosmos-sdk/version.AppName=$(APPNAME)d \
	-X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
	-X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT)

BUILD_FLAGS := -ldflags '$(ldflags)'

##############
###  Test  ###
##############

test-unit:
	@echo Running unit tests...
	@go test -mod=readonly -v -timeout 30m ./...

test-race:
	@echo Running unit tests with race condition reporting...
	@go test -mod=readonly -v -race -timeout 30m ./...

test-cover:
	@echo Running unit tests and creating coverage report...
	@go test -mod=readonly -v -timeout 30m -coverprofile=$(COVER_FILE) -covermode=atomic ./...
	@go tool cover -html=$(COVER_FILE) -o $(COVER_HTML_FILE)
	@rm $(COVER_FILE)

bench:
	@echo Running unit tests with benchmarking...
	@go test -mod=readonly -v -timeout 30m -bench=. ./...

test: govet govulncheck test-unit

.PHONY: test test-unit test-race test-cover bench

#################
###  Install  ###
#################

all: install

install:
	@echo "--> ensure dependencies have not been modified"
	@go mod verify
	@echo "--> installing $(APPNAME)d"
	@go install $(BUILD_FLAGS) -mod=readonly ./cmd/$(APPNAME)d

.PHONY: all install

##################
###  Protobuf  ###
##################

# Use this target if you do not want to use Ignite for generating proto files

proto-deps:
	@echo "Installing proto deps"
	@echo "Proto deps present, run 'go tool' to see them"

proto-gen:
	@echo "Generating protobuf files..."
	@ignite generate proto-go --yes

.PHONY: proto-gen

#################
###  Linting  ###
#################

lint:
	@echo "--> Running linter"
	@go tool github.com/golangci/golangci-lint/cmd/golangci-lint run ./... --timeout 15m

lint-fix:
	@echo "--> Running linter and fixing issues"
	@go tool github.com/golangci/golangci-lint/cmd/golangci-lint run ./... --fix --timeout 15m

.PHONY: lint lint-fix

###################
### Development ###
###################

govet:
	@echo Running go vet...
	@go vet ./...

govulncheck:
	@echo Running govulncheck...
	@go tool golang.org/x/vuln/cmd/govulncheck@latest
	@govulncheck ./...

.PHONY: govet govulncheck


###############
###  Build  ###
###############

# Default build uses testparams for integration testing.
# Use build-devnet, build-testnet, or build-mainnet for other environments.
build: build-test

build-test:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '$(ldflags) -X github.com/cosmos/cosmos-sdk/version.BuildTags=testparams' -tags testparams -o ./build/sparkdreamd ./cmd/sparkdreamd/main.go

build-devnet:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '$(ldflags) -X github.com/cosmos/cosmos-sdk/version.BuildTags=devnet' -tags devnet -o ./build/sparkdreamd ./cmd/sparkdreamd/main.go

build-testnet:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '$(ldflags) -X github.com/cosmos/cosmos-sdk/version.BuildTags=testnet' -tags testnet -o ./build/sparkdreamd ./cmd/sparkdreamd/main.go

build-mainnet:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '$(ldflags) -X github.com/cosmos/cosmos-sdk/version.BuildTags=mainnet' -tags mainnet -o ./build/sparkdreamd ./cmd/sparkdreamd/main.go

.PHONY: build build-test build-devnet build-testnet build-mainnet

##########################
###  Genesis Audit     ###
##########################

# Verify each network's genesis.json has every Params field expected by the
# matching build tag, plus cross-network ordering invariants. Run after any
# param changes or genesis regeneration. devnet/mainnet per-network targets
# activate once those genesis files are committed; the cross target skips
# its subtests automatically while a network's genesis is still missing.
verify-genesis: verify-genesis-devnet verify-genesis-testnet verify-genesis-mainnet verify-genesis-cross

verify-genesis-devnet:
	go test -count=1 -tags devnet ./deploy/config/network/devnet/...

verify-genesis-testnet:
	go test -count=1 -tags testnet ./deploy/config/network/testnet/...

verify-genesis-mainnet:
	go test -count=1 -tags mainnet ./deploy/config/network/mainnet/...

verify-genesis-cross:
	go test -count=1 ./deploy/config/network/crossnetwork/...

.PHONY: verify-genesis verify-genesis-devnet verify-genesis-testnet verify-genesis-mainnet verify-genesis-cross

##################
###  Checksum  ###
##################

do-checksum:
	cd build && sha256sum sparkdreamd > sparkdreamd-checksum

build-with-checksum: build-with-checksum-test
build-with-checksum-test: build-test do-checksum
build-with-checksum-devnet: build-devnet do-checksum
build-with-checksum-testnet: build-testnet do-checksum
build-with-checksum-mainnet: build-mainnet do-checksum

.PHONY: do-checksum build-with-checksum build-with-checksum-test build-with-checksum-devnet build-with-checksum-testnet build-with-checksum-mainnet

################
###  Docker  ###
################

docker-build: docker-build-test

docker-build-test: build-test
	docker build -f deploy/docker/Dockerfile-sparkdreamd-alpine -t sparkdreamnft/sparkdreamd-test:$(VERSION) .

docker-build-devnet: build-devnet
	docker build -f deploy/docker/Dockerfile-sparkdreamd-alpine -t sparkdreamnft/sparkdreamd-devnet:$(VERSION) .

docker-build-testnet: build-testnet
	docker build -f deploy/docker/Dockerfile-sparkdreamd-alpine -t sparkdreamnft/sparkdreamd-testnet:$(VERSION) .

docker-build-mainnet: build-mainnet
	docker build -f deploy/docker/Dockerfile-sparkdreamd-alpine -t sparkdreamnft/sparkdreamd-mainnet:$(VERSION) .

.PHONY: docker-build docker-build-test docker-build-devnet docker-build-testnet docker-build-mainnet

######################
###  Docker + SSH  ###
######################

docker-build-ssh: docker-build-test-ssh

docker-build-test-ssh: docker-build-test
	docker build --build-arg BASE_IMAGE=sparkdreamnft/sparkdreamd-test:$(VERSION) -f deploy/docker/Dockerfile-sparkdreamd-alpine-ssh -t sparkdreamnft/sparkdreamd-test-ssh:$(VERSION) .

docker-build-devnet-ssh: docker-build-devnet
	docker build --build-arg BASE_IMAGE=sparkdreamnft/sparkdreamd-devnet:$(VERSION) -f deploy/docker/Dockerfile-sparkdreamd-alpine-ssh -t sparkdreamnft/sparkdreamd-devnet-ssh:$(VERSION) .

docker-build-testnet-ssh: docker-build-testnet
	docker build --build-arg BASE_IMAGE=sparkdreamnft/sparkdreamd-testnet:$(VERSION) -f deploy/docker/Dockerfile-sparkdreamd-alpine-ssh -t sparkdreamnft/sparkdreamd-testnet-ssh:$(VERSION) .

docker-build-mainnet-ssh: docker-build-mainnet
	docker build --build-arg BASE_IMAGE=sparkdreamnft/sparkdreamd-mainnet:$(VERSION) -f deploy/docker/Dockerfile-sparkdreamd-alpine-ssh -t sparkdreamnft/sparkdreamd-mainnet-ssh:$(VERSION) .

.PHONY: docker-build-ssh docker-build-test-ssh docker-build-devnet-ssh docker-build-testnet-ssh docker-build-mainnet-ssh