#!/bin/bash

# AzNetMon - Quick Deployment Script

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🌐 AzNetMon - ICMP Network Monitor${NC}"
echo -e "${BLUE}====================================${NC}"

# Default values
DEFAULT_TARGETS="8.8.8.8,1.1.1.1,cloudflare.com,google.com"
DEFAULT_PORT="8080"

# Parse command line arguments
TARGETS="${1:-$DEFAULT_TARGETS}"
PORT="${2:-$DEFAULT_PORT}"

echo -e "${YELLOW}Targets:${NC} $TARGETS"
echo -e "${YELLOW}Port:${NC} $PORT"
echo ""

# Check if Docker is running
if ! docker info >/dev/null 2>&1; then
    echo -e "${RED}❌ Docker is not running. Please start Docker and try again.${NC}"
    exit 1
fi

# Build the image
echo -e "${BLUE}🔨 Building Docker image...${NC}"
docker build -t aznetmon:latest .

# Stop any existing container
echo -e "${BLUE}🛑 Stopping any existing containers...${NC}"
docker stop aznetmon 2>/dev/null || true
docker rm aznetmon 2>/dev/null || true

# Run the container
echo -e "${BLUE}🚀 Starting AzNetMon container...${NC}"
docker run -d \
    --name aznetmon \
    --cap-add=NET_RAW \
    -p ${PORT}:8080 \
    -e ICMP_TARGETS="${TARGETS}" \
    --restart unless-stopped \
    aznetmon:latest

# Wait a moment for the container to start
sleep 2

# Check if container is running
if docker ps | grep -q aznetmon; then
    echo -e "${GREEN}✅ AzNetMon is running successfully!${NC}"
    echo -e "${GREEN}📊 Dashboard: http://localhost:${PORT}${NC}"
    echo -e "${GREEN}📊 API: http://localhost:${PORT}/api/results${NC}"
    echo ""
    echo -e "${YELLOW}Commands:${NC}"
    echo -e "  ${BLUE}View logs:${NC} docker logs aznetmon"
    echo -e "  ${BLUE}Stop:${NC} docker stop aznetmon"
    echo -e "  ${BLUE}Restart:${NC} docker restart aznetmon"
else
    echo -e "${RED}❌ Failed to start AzNetMon. Check the logs:${NC}"
    echo -e "docker logs aznetmon"
    exit 1
fi
