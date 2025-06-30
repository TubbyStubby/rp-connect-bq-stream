# Makefile for rp-connect-bq-stream
# Provides convenient targets for building and releasing Docker images

.PHONY: help build build-local release-patch release-minor release-major dry-run clean docker-login

# Default target
help:
	@echo "Available targets:"
	@echo "  help           - Show this help message"
	@echo "  build          - Build Docker image locally (no push)"
	@echo "  build-local    - Same as build (alias)"
	@echo "  release-patch  - Build and release with patch version increment"
	@echo "  release-minor  - Build and release with minor version increment"
	@echo "  release-major  - Build and release with major version increment"
	@echo "  dry-run        - Show what would be done without executing"
	@echo "  clean          - Remove local Docker images"
	@echo "  docker-login   - Login to DockerHub"
	@echo ""
	@echo "Examples:"
	@echo "  make build          # Build locally for testing"
	@echo "  make release-patch  # Release v1.2.3 -> v1.2.4"
	@echo "  make dry-run        # Preview next release"

# Build locally without pushing
build:
	@echo "🔨 Building Docker image locally..."
	./scripts/build-and-publish.sh --no-push

build-local: build

# Release targets
release-patch:
	@echo "🚀 Creating patch release..."
	./scripts/build-and-publish.sh -t patch -f

release-minor:
	@echo "🚀 Creating minor release..."
	./scripts/build-and-publish.sh -t minor -f

release-major:
	@echo "🚀 Creating major release..."
	./scripts/build-and-publish.sh -t major -f

# Preview what would happen
dry-run:
	@echo "👀 Previewing next release..."
	./scripts/build-and-publish.sh --dry-run

# Clean up local images
clean:
	@echo "🧹 Cleaning up local Docker images..."
	-docker rmi tubbystubby/rp-connect-bq-stream:latest 2>/dev/null || true
	-docker images tubbystubby/rp-connect-bq-stream --format "table {{.Repository}}:{{.Tag}}\t{{.ID}}" | grep -v REPOSITORY | awk '{print $$2}' | xargs -r docker rmi 2>/dev/null || true
	@echo "✅ Cleanup complete"

# Login to DockerHub
docker-login:
	@echo "🔐 Logging into DockerHub..."
	docker login

# Development helpers
dev-build: build
	@echo "✅ Development build complete"

# Check prerequisites
check-prereqs:
	@echo "🔍 Checking prerequisites..."
	@command -v docker >/dev/null 2>&1 || { echo "❌ Docker is required but not installed"; exit 1; }
	@command -v git >/dev/null 2>&1 || { echo "❌ Git is required but not installed"; exit 1; }
	@docker info >/dev/null 2>&1 || { echo "❌ Docker daemon is not running"; exit 1; }
	@git rev-parse --git-dir >/dev/null 2>&1 || { echo "❌ Not in a Git repository"; exit 1; }
	@echo "✅ All prerequisites met"
