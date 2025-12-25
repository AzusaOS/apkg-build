# apkg-build

A package build system for [Azusa OS](https://github.com/AzusaOS). This tool automates the process of downloading, building, and packaging software into SquashFS archives for the Azusa package management system.

## Features

- **Multiple build engines**: Supports autoconf, CMake, Meson, and custom build scripts
- **Automatic engine detection**: Detects the appropriate build system from source files
- **Cross-architecture builds**: Build packages for amd64, 386, and arm64 via QEMU
- **Reproducible builds**: Uses `SOURCE_DATE_EPOCH` and deterministic archive options
- **Smart caching**: Downloads are cached locally and mirrored to S3
- **Automatic organization**: Separates output into core, libs, dev, doc, and fonts subpackages

## Installation

```bash
go build -o apkg-build .
```

## Usage

```bash
# Update the recipe repository
apkg-build update

# Build a package by name
apkg-build build zlib

# Build a package by category/name
apkg-build build sys-libs/zlib

# Build a specific version
apkg-build -version 1.2.13 build sys-libs/zlib

# Build for a different architecture
apkg-build -arch arm64 build sys-libs/zlib
```

## Recipe Repository

apkg-build uses recipes from the [azusa-opensource-recipes](https://github.com/AzusaOS/azusa-opensource-recipes) repository. The repository is automatically cloned to one of these locations:

- `$HOME/apkg-recipes`
- `$HOME/projects/apkg-recipes`
- `$HOME/.cache/apkg-recipes`
- `/tmp/apkg-recipes`

## Package Structure

Each package recipe is a directory containing:

```
category/package-name/
├── build.yaml      # Build configuration
├── metadata.yaml   # File hashes (auto-generated)
├── azusa.yaml      # Optional package metadata
└── files/          # Patches and additional files
    └── *.patch
```

## build.yaml Format

```yaml
versions:
  list:
    - "1.0.0"
    - "1.1.0"
    - "1.2.0"
  stable: "1.2.0"

build:
  - version: "1.*"              # Version pattern (glob)
    source:
      - "https://example.com/pkg-1.2.0.tar.gz"
      - "https://example.com/extra.zip -> extra.zip"  # Rename syntax

    patches:
      - "001-fix-build.patch"   # From files/ directory

    engine: "autoconf"          # autoconf, cmake, meson, none, or auto

    options:
      - autoreconf              # Run autoreconf -fi
      - light                   # Skip standard configure flags
      - build_in_tree           # Build in source directory

    arguments:
      - "--enable-shared"
      - "--disable-static"

    env:
      - "CFLAGS=-O2"
      - "S=${WORKDIR}/custom-dir"  # Override source directory

    import:
      - "sys-libs/zlib:1.2"     # Package dependency with version
      - "libpng"                # pkg-config dependency

    # Build hooks (shell commands)
    configure_pre: []
    configure_post: []
    compile_pre: []
    compile_post: []
    install_pre: []
    install_post: []
```

## Shell Build Scripts

For packages without a `build.yaml`, apkg-build supports legacy shell scripts. These are self-contained bash scripts that handle the entire build process.

### Script Format

Scripts are named `{package}-{version}.sh` (e.g., `zlib-1.0.8.sh`) and placed in the package directory:

```
category/package-name/
├── zlib-1.0.8.sh
├── zlib-1.2.13.sh
└── files/
    └── patches...
```

### Script Structure

Shell scripts source `common/init.sh` which provides helper functions:

```bash
#!/bin/sh
source "../../common/init.sh"

get https://example.com/source.tar.gz    # Download and extract
acheck                                     # Verify build environment

cd "${T}"                                  # Change to temp build dir
importpkg sys-libs/zlib                   # Import dependencies
doconf --enable-shared                    # Run configure
make -j${NPROC}                           # Build
make install DESTDIR="${D}"               # Install

finalize                                   # Run fixelf, organize, archive
```

### Available Functions

| Function | Description |
|----------|-------------|
| `get URL [filename]` | Download and extract source |
| `doconf [args]` | Run configure with standard paths |
| `doconflight [args]` | Run configure with minimal paths |
| `docmake [args]` | CMake build |
| `domeson [args]` | Meson build |
| `importpkg pkg...` | Import package dependencies |
| `apatch file...` | Apply patches |
| `aautoreconf` | Run autoreconf |
| `finalize` | Run fixelf, organize, archive |

## Build Engines

### autoconf

Standard GNU Autotools build:
```
./configure --prefix=... && make && make install
```

Options:
- `autoreconf`: Run `autoreconf -fi` before configure
- `light`: Skip standard configure flags
- `213`: Skip `--docdir` flag (for older autoconf)
- `build_in_tree`: Build in source directory instead of separate build dir

### cmake

CMake with Ninja generator:
```
cmake -G Ninja ... && ninja && ninja install
```

### meson

Meson build system:
```
meson setup ... && ninja && ninja install
```

### none

No automatic build commands. Use hooks (`compile_pre`, `install_pre`, etc.) to define custom build steps.

## Build Variables

The following variables are available in build.yaml and hooks:

| Variable | Description |
|----------|-------------|
| `$P` | Package name with version (e.g., `zlib-1.2.13`) |
| `$PN` | Package name (e.g., `zlib`) |
| `$PV` | Package version (e.g., `1.2.13`) |
| `$CATEGORY` | Package category (e.g., `sys-libs`) |
| `$WORKDIR` | Working directory for extracted sources |
| `$S` | Source directory (auto-detected or set via env) |
| `$D` | Destination directory (DESTDIR) |
| `$T` | Temporary build directory |
| `$FILESDIR` | Path to package's files/ directory |
| `$CHOST` | Target host triplet (e.g., `x86_64-pc-linux-gnu`) |
| `$ARCH` | Target architecture (amd64, 386, arm64) |
| `$LIBSUFFIX` | Library suffix (64 for amd64, empty otherwise) |

## Output Structure

Built packages are organized into subpackages:

| Subpackage | Contents |
|------------|----------|
| `.core` | Executables, runtime libraries, configuration |
| `.libs` | Shared libraries |
| `.dev` | Headers, static libraries, pkg-config, cmake files |
| `.doc` | Man pages, info files, documentation |
| `.fonts` | Font files |
| `.mod.pyX.Y` | Python version-specific modules |

Output location:
- SquashFS files: `/tmp/apkg/`
- If running as root: Also copied to `/var/lib/apkg/unsigned/`

## Cross-Architecture Builds

For non-native architectures, apkg-build automatically launches a QEMU virtual machine:

- **amd64/386**: Uses KVM acceleration (8GB RAM)
- **arm64**: Software emulation (2GB RAM)

QEMU VMs are configured with:
- Temporary disk image for build artifacts
- SSH access for remote command execution
- Network access for package downloads

## Requirements

- Go 1.16+
- For local builds:
  - Standard build tools (gcc, make, etc.)
  - mksquashfs
- For QEMU builds:
  - QEMU with KVM support
  - Azusa kernel and initrd

## License

See the Azusa OS project for licensing information.
