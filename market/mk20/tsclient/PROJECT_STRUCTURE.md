# Project Structure

```
tsclient/
├── src/                          # Source code
│   ├── index.ts                  # Main exports
│   └── client.ts                 # Custom client wrapper
├── tests/                        # Test files
│   ├── setup.ts                  # Test configuration
│   └── client.test.ts            # Client tests
├── examples/                     # Usage examples
│   └── basic-usage.ts            # Basic client usage
├── scripts/                      # Build scripts
│   └── build.sh                  # Automated build script
├── generated/                    # Auto-generated client (from swagger)
├── dist/                         # Compiled output
├── package.json                  # Dependencies and scripts
├── tsconfig.json                 # TypeScript configuration
├── jest.config.js                # Jest test configuration
├── Makefile                      # Build targets
├── .gitignore                    # Git ignore rules
├── README.md                     # Main documentation
└── PROJECT_STRUCTURE.md          # This file
```

## Key Components

### 1. Generated Client (`generated/`)
- **Source**: Generated from `../http/swagger.json`
- **Tool**: OpenAPI Generator CLI
- **Language**: TypeScript with fetch API
- **Purpose**: Provides the raw API interface

### 2. Custom Client Wrapper (`src/client.ts`)
- **Purpose**: User-friendly interface over generated client
- **Features**: 
  - Simplified method names
  - Better error handling
  - Consistent return types
  - Type safety

### 3. Main Exports (`src/index.ts`)
- **Purpose**: Clean public API
- **Exports**: 
  - Generated types and client
  - Custom client wrapper
  - Configuration interfaces

### 4. Build System
- **Package Manager**: npm
- **Build Tool**: TypeScript compiler
- **Code Generation**: OpenAPI Generator
- **Testing**: Jest
- **Automation**: Makefile + shell scripts

## Build Process

1. **Generate**: `swagger.json` → TypeScript client
2. **Compile**: TypeScript → JavaScript
3. **Package**: Output to `dist/` directory

## Development Workflow

1. **Setup**: `make setup` or `./scripts/build.sh`
2. **Development**: Edit source files in `src/`
3. **Regenerate**: `npm run generate` (when API changes)
4. **Build**: `npm run build` or `make build`
5. **Test**: `npm test` or `make test`

## File Purposes

- **`package.json`**: Dependencies, scripts, metadata
- **`tsconfig.json`**: TypeScript compiler options
- **`jest.config.js`**: Test framework configuration
- **`Makefile`**: Build automation targets
- **`build.sh`**: Automated build script
- **`.gitignore`**: Version control exclusions
- **`README.md`**: User documentation
- **`PROJECT_STRUCTURE.md`**: This file (developer reference)
