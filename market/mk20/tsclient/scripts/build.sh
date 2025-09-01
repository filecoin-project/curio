#!/bin/bash

set -e

echo "🚀 Building Curio TypeScript Market Client..."

# Check if Node.js is installed
if ! command -v node &> /dev/null; then
    echo "❌ Node.js is not installed. Please install Node.js 18+ first."
    exit 1
fi

# Check Node.js version
NODE_VERSION=$(node -v | cut -d'v' -f2 | cut -d'.' -f1)
if [ "$NODE_VERSION" -lt 18 ]; then
    echo "❌ Node.js version 18+ is required. Current version: $(node -v)"
    exit 1
fi

echo "✅ Node.js version: $(node -v)"

# Check if npm is installed
if ! command -v npm &> /dev/null; then
    echo "❌ npm is not installed. Please install npm first."
    exit 1
fi

echo "✅ npm version: $(npm -v)"

# Clean previous builds
echo "🧹 Cleaning previous builds..."
npm run clean

# Install dependencies
echo "📦 Installing dependencies..."
npm install

# Generate client from swagger
echo "🔧 Generating TypeScript client from swagger files..."
npm run generate

# Compile TypeScript
echo "⚙️  Compiling TypeScript..."
npm run compile

echo "✅ Build completed successfully!"
echo ""
echo "📁 Generated files:"
echo "  - Generated client: ./generated/"
echo "  - Compiled output: ./dist/"
echo "  - Type definitions: ./dist/index.d.ts"
echo ""
echo "🚀 You can now use the client:"
echo "  import { Client } from '@curio/market-client';"
echo ""
echo "📚 See examples/ for usage examples"
