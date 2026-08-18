#!/bin/bash

# Claude Code Monitor - Build and Run Script

set -e

echo "🚀 Claude Code Monitor - Starting Services"
echo "========================================="

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.20 or higher."
    exit 1
fi

# Check if Node.js is installed
if ! command -v node &> /dev/null; then
    echo "❌ Node.js is not installed. Please install Node.js 20 or higher."
    exit 1
fi

NODE_MAJOR=$(node -p 'process.versions.node.split(".")[0]')
if [ "$NODE_MAJOR" -lt 20 ]; then
    echo "❌ Node.js 20 or higher is required. Found: $(node --version)"
    exit 1
fi

if ! command -v npm &> /dev/null; then
    echo "❌ npm is not installed. Install it with Node.js 20 or higher."
    exit 1
fi

# Check for .env file
if [ ! -f .env ]; then
    echo -e "${YELLOW}⚠️  No .env file found. Creating from .env.example...${NC}"
    if [ -f .env.example ]; then
        cp .env.example .env
        echo -e "${GREEN}✅ Created .env file.${NC}"
    else
        echo "❌ No .env.example file found."
        exit 1
    fi
fi

# Function to cleanup on exit
cleanup() {
    echo -e "\n${YELLOW}Shutting down services...${NC}"
    kill $PROXY_PID $WEB_PID 2>/dev/null || true
    exit
}

trap cleanup EXIT INT TERM

# Build and start proxy server
echo -e "\n${BLUE}📦 Building proxy server...${NC}"
mkdir -p bin
cd proxy
go mod download
go build -o ../bin/proxy cmd/proxy/main.go
cd ..

echo -e "${GREEN}✅ Proxy server built${NC}"

# Install web dependencies if needed
if [ ! -d "web/node_modules" ]; then
    echo -e "\n${BLUE}📦 Installing web dependencies...${NC}"
    cd web
    npm ci
    cd ..
    echo -e "${GREEN}✅ Web dependencies installed${NC}"
fi

# Start proxy server
echo -e "\n${BLUE}🚀 Starting proxy server on port 3001...${NC}"
./bin/proxy &
PROXY_PID=$!

# Wait for proxy to start
sleep 2

# Start web server
echo -e "${BLUE}🚀 Starting web interface on port 5173...${NC}"
cd web
npm run dev &
WEB_PID=$!
cd ..

echo -e "\n${GREEN}✨ All services started!${NC}"
echo "========================================="
echo -e "📊 Web Dashboard: ${BLUE}http://localhost:5173${NC}"
echo -e "🔌 API Proxy: ${BLUE}http://localhost:3001${NC}"
echo -e "💚 Health Check: ${BLUE}http://localhost:3001/health${NC}"
echo "========================================="
echo -e "${YELLOW}Press Ctrl+C to stop all services${NC}\n"

# Wait for processes
wait
