#!/bin/bash

# Convenience script for local development builds
# This is a wrapper around the main build-and-publish.sh script

set -e

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔨 Starting local development build...${NC}"
echo -e "${GREEN}This will build the Docker image locally without pushing to registry${NC}"
echo -e "${YELLOW}No Git tags will be created or pushed${NC}"
echo

# Change to project root and execute the main build script
cd "$PROJECT_ROOT"
exec "$SCRIPT_DIR/build-and-publish.sh" --no-push
