# Build and Publish Documentation

This document describes how to use the `build-and-publish.sh` script to build and publish Docker images for the `rp-connect-bq-stream` project.

## Prerequisites

1. **Docker**: Ensure Docker is installed and running
2. **Git**: Must be in a Git repository with proper remote setup
3. **DockerHub Access**: Must be logged into DockerHub (`docker login`)
4. **Permissions**: Script must be executable (`chmod +x build-and-publish.sh`)

## Quick Start

```bash
# Simple build and publish with patch increment
./scripts/build-and-publish.sh

# Build with minor version increment
./scripts/build-and-publish.sh -t minor

# Build locally without pushing
./scripts/build-and-publish.sh --no-push

# Preview what would happen
./scripts/build-and-publish.sh --dry-run
```

## Usage

```
./scripts/build-and-publish.sh [OPTIONS]
```

### Options

| Option | Description | Default |
|--------|-------------|---------|
| `-h, --help` | Show help message | - |
| `-t, --type TYPE` | Version increment type: `patch`, `minor`, `major` | `patch` |
| `-n, --no-push` | Build locally without pushing to registry | `false` |
| `-f, --force` | Skip confirmation prompts | `false` |
| `--dry-run` | Show what would be done without executing | `false` |

### Version Increment Types

- **patch**: `v1.2.3` → `v1.2.4` (bug fixes)
- **minor**: `v1.2.3` → `v1.3.0` (new features, backward compatible)
- **major**: `v1.2.3` → `v2.0.0` (breaking changes)

## What the Script Does

1. **Validation**:
   - Checks if you're in a Git repository
   - Warns about uncommitted changes
   - Validates version format

2. **Version Management**:
   - Retrieves the latest Git tag
   - Increments version based on selected type
   - Creates new semantic version tag

3. **Docker Build**:
   - Updates Dockerfile source label to point to specific commit
   - Adds version label to image
   - Builds image with both versioned and `latest` tags

4. **Publishing** (if not using `--no-push`):
   - Creates and pushes Git tag
   - Pushes Docker images to DockerHub

## Examples

### Interactive Mode (Default)
```bash
./scripts/build-and-publish.sh
```
- Prompts for version increment type
- Asks for confirmation before pushing

### Automated Minor Release
```bash
./scripts/build-and-publish.sh -t minor -f
```
- Increments minor version automatically
- Skips all confirmation prompts

### Local Development Build
```bash
./scripts/build-and-publish.sh --no-push
```
- Builds images locally
- Doesn't create Git tags or push to registry

### Preview Changes
```bash
./scripts/build-and-publish.sh --dry-run
```
- Shows what would happen without executing
- Useful for CI/CD pipeline testing

## Image Tags

The script creates two Docker image tags:
- `tubbystubby/rp-connect-bq-stream:vX.Y.Z` (specific version)
- `tubbystubby/rp-connect-bq-stream:latest` (latest version)

## Labels Added to Images

The script automatically adds these OCI labels:
- `org.opencontainers.image.source`: Points to the specific commit

## Git Workflow

### Recommended Workflow
1. Make your changes
2. Commit changes to Git
3. Run the build script:
   ```bash
   ./scripts/build-and-publish.sh -t patch
   ```
4. Script will:
   - Create a new Git tag
   - Build and push Docker images
   - Push the Git tag to origin

### Working with Uncommitted Changes
The script will warn about uncommitted changes but allows you to continue. This is useful for testing builds before committing.

## Troubleshooting

### Common Issues

1. **"Not in a git repository"**
   - Ensure you're in the project root directory
   - Check that `.git` directory exists

2. **"Latest tag doesn't follow semantic versioning"**
   - Ensure existing tags follow `vX.Y.Z` format
   - Use `git tag -d <tag>` to remove invalid tags

3. **Docker build fails**
   - Check Dockerfile syntax
   - Ensure all required files are present
   - Verify Docker daemon is running

4. **Push to DockerHub fails**
   - Login to DockerHub: `docker login`
   - Verify image name and repository permissions

### Debugging

Use the `--dry-run` flag to see what the script would do:
```bash
./scripts/build-and-publish.sh --dry-run
```

## Security Notes

- The script doesn't hardcode any credentials
- Relies on existing Docker login session
- Git credentials are handled by your Git configuration
- Source labels point to public GitHub repository

## CI/CD Integration

For automated builds in CI/CD pipelines:

```bash
# Example CI/CD usage
./scripts/build-and-publish.sh -t patch -f
```

This skips interactive prompts and automatically increments the patch version.
