#!/bin/bash

# NextDNS Cross-Platform Build Script
# Builds stripped binaries for all supported platforms

# Don't exit on error for individual builds
set +e

VERSION="${VERSION:-2.0.0}"
BUILD_DIR="build"
DIST_DIR="dist"

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}╔════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  NextDNS Cross-Platform Release Builder          ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════╝${NC}"
echo ""
echo "Version: $VERSION"
echo ""

# Clean previous builds
rm -rf "$BUILD_DIR" "$DIST_DIR"
mkdir -p "$BUILD_DIR" "$DIST_DIR"

# Build flags
LDFLAGS="-s -w -X main.version=$VERSION"

# Platform definitions: OS/ARCH
PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "linux/arm/7"
    "freebsd/amd64"
    "freebsd/arm64"
)

total=${#PLATFORMS[@]}
current=0

build_platform() {
    local platform=$1
    local goos=${platform%%/*}
    local rest=${platform#*/}
    local goarch goarm arch_name

    # Handle ARM variants
    if [[ "$rest" == *"/"* ]]; then
        goarch=${rest%%/*}
        goarm=${rest#*/}
        export GOARM=$goarm
        arch_name="armv${goarm}"
    else
        goarch="$rest"
        unset GOARM
        arch_name="$goarch"
    fi

    current=$((current + 1))

    echo -e "${BLUE}[$current/$total]${NC} Building ${goos}/${arch_name}..."

    # Set build environment
    export GOOS=$goos
    export GOARCH=$goarch
    export CGO_ENABLED=0

    # Output binary name
    binary_name="nextdns"
    if [ "$goos" = "windows" ]; then
        binary_name="nextdns.exe"
    fi

    # Build directory for this platform
    platform_dir="$BUILD_DIR/${goos}_${arch_name}"
    mkdir -p "$platform_dir"

    # Build
    if go build -ldflags "$LDFLAGS" -o "$platform_dir/$binary_name" .; then
        # Get binary size
        size=$(ls -lh "$platform_dir/$binary_name" | awk '{print $5}')
        echo -e "  ${GREEN}✓${NC} Built successfully ($size)"

        # Create archive
        archive_name="nextdns_${VERSION}_${goos}_${arch_name}.tar.gz"
        (cd "$platform_dir" && tar czf "../../$DIST_DIR/$archive_name" "$binary_name")

        archive_size=$(ls -lh "$DIST_DIR/$archive_name" | awk '{print $5}')
        echo -e "  ${GREEN}✓${NC} Archived: $archive_name ($archive_size)"

        return 0
    else
        echo -e "  ${RED}✗${NC} Build failed"
        return 1
    fi
}

# Build for all platforms
echo "Building for ${#PLATFORMS[@]} platforms..."
echo ""

failed=0
succeeded=0

for platform in "${PLATFORMS[@]}"; do
    if build_platform "$platform"; then
        ((succeeded++))
    else
        ((failed++))
    fi
    echo ""
done

# Summary
echo -e "${BLUE}╔════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  Build Summary                                     ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "Total platforms: $total"
echo -e "${GREEN}Succeeded: $succeeded${NC}"
if [ $failed -gt 0 ]; then
    echo -e "${RED}Failed: $failed${NC}"
fi
echo ""

# List all archives
echo "Generated archives:"
echo ""
ls -lh "$DIST_DIR" | tail -n +2 | awk '{printf "  %s  %s\n", $5, $9}'
echo ""

# Calculate total size
total_size=$(du -sh "$DIST_DIR" | cut -f1)
echo "Total size: $total_size"
echo ""

# Create checksums
echo "Generating checksums..."
(cd "$DIST_DIR" && sha256sum *.tar.gz > SHA256SUMS)
echo -e "${GREEN}✓${NC} SHA256SUMS created"
echo ""

echo -e "${GREEN}╔════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║  Build Complete! 🎉                                ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════════════════╝${NC}"
echo ""
echo "Archives location: $DIST_DIR/"
echo ""
echo "Next steps:"
echo "  1. Test a binary:"
echo "     tar xzf $DIST_DIR/nextdns_${VERSION}_linux_amd64.tar.gz"
echo "     ./nextdns version"
echo ""
echo "  2. Create GitHub release:"
echo "     git tag -a v${VERSION} -m 'Release v${VERSION}'"
echo "     git push origin v${VERSION}"
echo ""
echo "  3. Upload archives to GitHub release"
echo ""
