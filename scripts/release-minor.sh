#!/bin/bash

# Convenience script for minor releases
# This is a wrapper around the main build-and-publish.sh script

set -e

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🚀 Starting minor release build...${NC}"
echo -e "${GREEN}This will increment the minor version (e.g., v1.2.3 → v1.3.0)${NC}"
echo

# Change to project root and execute the main build script
cd "$PROJECT_ROOT"
exec "$SCRIPT_DIR/build-and-publish.sh" -t minor -f
