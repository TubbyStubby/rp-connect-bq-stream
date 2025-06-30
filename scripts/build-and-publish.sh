#!/bin/bash

set -e

# Help function
show_help() {
    cat << EOF
Docker Build and Publish Script

USAGE:
    $0 [OPTIONS]

OPTIONS:
    -h, --help          Show this help message
    -t, --type TYPE     Version increment type: patch, minor, major (default: patch)
    -n, --no-push       Build and tag locally without pushing to registry
    -f, --force         Skip confirmation prompts
    --dry-run          Show what would be done without executing

EXAMPLES:
    $0                  # Interactive mode with patch increment
    $0 -t minor         # Increment minor version
    $0 -t major -f      # Increment major version without prompts
    $0 --no-push        # Build locally only
    $0 --dry-run        # Preview changes

EOF
}

# Parse command line arguments
INCREMENT_TYPE="patch"
NO_PUSH=false
FORCE=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -t|--type)
            INCREMENT_TYPE="$2"
            if [[ ! "$INCREMENT_TYPE" =~ ^(patch|minor|major)$ ]]; then
                print_error "Invalid increment type: $INCREMENT_TYPE. Use: patch, minor, or major"
                exit 1
            fi
            shift 2
            ;;
        -n|--no-push)
            NO_PUSH=true
            shift
            ;;
        -f|--force)
            FORCE=true
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
IMAGE_NAME="tubbystubby/rp-connect-bq-stream"
DOCKERFILE_PATH="./Dockerfile"

# Ensure we're running from project root
if [[ ! -f "Dockerfile" ]]; then
    print_error "Dockerfile not found. Please run this script from the project root directory."
    print_error "Usage: ./scripts/build-and-publish.sh"
    exit 1
fi

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if we're in a git repository
if ! git rev-parse --git-dir > /dev/null 2>&1; then
    print_error "Not in a git repository"
    exit 1
fi

# Check if working directory is clean
if [[ -n $(git status --porcelain) ]]; then
    print_warning "Working directory is not clean. Uncommitted changes:"
    git status --short
    if [[ "$FORCE" == false && "$DRY_RUN" == false ]]; then
        read -p "Do you want to continue? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            print_error "Aborted by user"
            exit 1
        fi
    fi
fi

# Get the latest tag
print_status "Getting latest version tag..."
LATEST_TAG=$(git tag --list --sort=-version:refname | head -1)

if [[ -z "$LATEST_TAG" ]]; then
    print_warning "No existing tags found. Starting with v0.0.1"
    NEW_TAG="v0.0.1"
else
    print_status "Latest tag: $LATEST_TAG"

    # Parse version components (assuming format vX.Y.Z)
    if [[ $LATEST_TAG =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
        MAJOR=${BASH_REMATCH[1]}
        MINOR=${BASH_REMATCH[2]}
        PATCH=${BASH_REMATCH[3]}

        # Determine increment type
        if [[ "$FORCE" == false && "$DRY_RUN" == false ]] && [[ -z "$2" ]]; then
            # Interactive mode
            echo "Current version: $LATEST_TAG"
            echo "Select increment type:"
            echo "1) Patch (${MAJOR}.${MINOR}.$((PATCH + 1)))"
            echo "2) Minor (${MAJOR}.$((MINOR + 1)).0)"
            echo "3) Major ($((MAJOR + 1)).0.0)"
            read -p "Enter choice (1-3) [default: 1]: " -n 1 -r
            echo

            case $REPLY in
                2) INCREMENT_TYPE="minor" ;;
                3) INCREMENT_TYPE="major" ;;
                *) INCREMENT_TYPE="patch" ;;
            esac
        fi

        case $INCREMENT_TYPE in
            minor)
                NEW_TAG="v${MAJOR}.$((MINOR + 1)).0"
                ;;
            major)
                NEW_TAG="v$((MAJOR + 1)).0.0"
                ;;
            patch|*)
                NEW_TAG="v${MAJOR}.${MINOR}.$((PATCH + 1))"
                ;;
        esac
    else
        print_error "Latest tag '$LATEST_TAG' doesn't follow semantic versioning (vX.Y.Z)"
        exit 1
    fi
fi

print_status "New version will be: $NEW_TAG"

# Get current commit hash
COMMIT_HASH=$(git rev-parse HEAD)
print_status "Current commit: $COMMIT_HASH"

if [[ "$DRY_RUN" == true ]]; then
    print_status "DRY RUN - Would perform the following actions:"
    print_status "  - Create git tag: $NEW_TAG"
    print_status "  - Update Dockerfile source label to: https://github.com/TubbyStubby/rp-connect-bq-stream/tree/$COMMIT_HASH"
    print_status "  - Build Docker image: ${IMAGE_NAME}:${NEW_TAG}"
    print_status "  - Build Docker image: ${IMAGE_NAME}:latest"
    if [[ "$NO_PUSH" == false ]]; then
        print_status "  - Push git tag: $NEW_TAG"
        print_status "  - Push Docker image: ${IMAGE_NAME}:${NEW_TAG}"
        print_status "  - Push Docker image: ${IMAGE_NAME}:latest"
    fi
    exit 0
fi

# Create a temporary Dockerfile with updated source label
print_status "Updating Dockerfile source label..."
TEMP_DOCKERFILE=$(mktemp)
cp "$DOCKERFILE_PATH" "$TEMP_DOCKERFILE"

# Update the source label to point to the specific commit
sed -i "s|LABEL org.opencontainers.image.source=\"https://github.com/TubbyStubby/rp-connect-bq-stream\"|LABEL org.opencontainers.image.source=\"https://github.com/TubbyStubby/rp-connect-bq-stream/tree/$COMMIT_HASH\"|" "$TEMP_DOCKERFILE"

print_status "Building Docker image..."
docker build -f "$TEMP_DOCKERFILE" -t "${IMAGE_NAME}:${NEW_TAG}" -t "${IMAGE_NAME}:latest" .

# Clean up temporary file
rm "$TEMP_DOCKERFILE"

print_success "Docker image built successfully"
print_status "Image tags: ${IMAGE_NAME}:${NEW_TAG}, ${IMAGE_NAME}:latest"

# Check if we should push
if [[ "$NO_PUSH" == true ]]; then
    print_warning "Skipping git tag creation and DockerHub push (--no-push flag)"
    print_status "Image built locally with tags:"
    print_status "  - ${IMAGE_NAME}:${NEW_TAG}"
    print_status "  - ${IMAGE_NAME}:latest"
    exit 0
fi

# Ask if user wants to create git tag and push (unless forced)
if [[ "$FORCE" == false ]]; then
    read -p "Create git tag '$NEW_TAG' and push to DockerHub? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_warning "Skipping git tag creation and DockerHub push"
        print_status "Image built locally with tags:"
        print_status "  - ${IMAGE_NAME}:${NEW_TAG}"
        print_status "  - ${IMAGE_NAME}:latest"
        exit 0
    fi
fi

# Create and push git tag
print_status "Creating git tag: $NEW_TAG"
git tag -a "$NEW_TAG" -m "Release $NEW_TAG"

print_status "Pushing git tag to origin..."
git push origin "$NEW_TAG"

# Push to DockerHub
print_status "Pushing to DockerHub..."
docker push "${IMAGE_NAME}:${NEW_TAG}"
docker push "${IMAGE_NAME}:latest"

print_success "Successfully built and published:"
print_success "  - Git tag: $NEW_TAG"
print_success "  - Docker image: ${IMAGE_NAME}:${NEW_TAG}"
print_success "  - Docker image: ${IMAGE_NAME}:latest"

# Show image information
echo
print_status "Image information:"
docker images "${IMAGE_NAME}" --format "table {{.Repository}}:{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}"
