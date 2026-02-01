#!/bin/bash

# NextDNS Cross-Compilation Script
# Builds stripped binaries for amd64, arm64, and armv7

VERSION="${VERSION:-2.0.0}"
DIST_DIR="dist"

echo "════════════════════════════════════════════════════"
echo "  NextDNS Cross-Platform Build"
echo "════════════════════════════════════════════════════"
echo "Version: $VERSION"
echo ""

# Clean and create dist directory
rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

# Build flags - stripped binaries
LDFLAGS="-s -w -X main.version=$VERSION"

build_binary() {
    local os=$1
    local arch=$2
    local arm_version=$3
    local arch_name=$4

    echo "Building ${os}/${arch_name}..."

    export GOOS=$os
    export GOARCH=$arch
    export CGO_ENABLED=0

    if [ -n "$arm_version" ]; then
        export GOARM=$arm_version
    fi

    binary_name="nextdns_${VERSION}_${os}_${arch_name}"

    if go build -ldflags "$LDFLAGS" -o "$DIST_DIR/$binary_name" . 2>&1; then
        size=$(ls -lh "$DIST_DIR/$binary_name" | awk '{print $5}')
        echo "  ✓ Success: $binary_name ($size)"

        # Create archive
        (cd "$DIST_DIR" && tar czf "${binary_name}.tar.gz" "$binary_name" && rm "$binary_name")
        archive_size=$(ls -lh "$DIST_DIR/${binary_name}.tar.gz" | awk '{print $5}')
        echo "  ✓ Archive: ${binary_name}.tar.gz ($archive_size)"
        echo ""
        return 0
    else
        echo "  ✗ Failed"
        echo ""
        return 1
    fi
}

# Build for Linux (most common platforms)
echo "Building Linux binaries..."
echo ""
build_binary "linux" "amd64" "" "amd64"
build_binary "linux" "arm64" "" "arm64"
build_binary "linux" "arm" "7" "armv7"

# Generate checksums
echo "Generating checksums..."
(cd "$DIST_DIR" && sha256sum *.tar.gz > SHA256SUMS 2>/dev/null)
echo "  ✓ SHA256SUMS created"
echo ""

# Summary
echo "════════════════════════════════════════════════════"
echo "  Build Complete!"
echo "════════════════════════════════════════════════════"
echo ""
echo "Generated files:"
ls -lh "$DIST_DIR" | tail -n +2 | awk '{printf "  %8s  %s\n", $5, $9}'
echo ""

total_size=$(du -sh "$DIST_DIR" | cut -f1)
echo "Total size: $total_size"
echo "Location: $DIST_DIR/"
echo ""
