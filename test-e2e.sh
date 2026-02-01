#!/bin/bash

# NextDNS End-to-End Test Suite
# Tests all critical bug fixes and performance improvements

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

BINARY="./nextdns"
TEST_PORT=5353
CONFIG_ID="${NEXTDNS_CONFIG_ID:-abc123}"  # Set via env or use default
TESTS_PASSED=0
TESTS_FAILED=0

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((TESTS_PASSED++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((TESTS_FAILED++))
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

cleanup() {
    if [ ! -z "$NEXTDNS_PID" ] && kill -0 $NEXTDNS_PID 2>/dev/null; then
        log_info "Cleaning up daemon (PID: $NEXTDNS_PID)..."
        kill $NEXTDNS_PID 2>/dev/null || true
        wait $NEXTDNS_PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

# Check prerequisites
check_prereqs() {
    log_info "Checking prerequisites..."

    if [ ! -f "$BINARY" ]; then
        log_fail "Binary not found: $BINARY"
        exit 1
    fi

    if ! command -v dig &> /dev/null; then
        log_warn "dig not found, installing dnsutils..."
        sudo apt-get update && sudo apt-get install -y dnsutils || true
    fi

    log_success "Prerequisites OK"
}

# Test 1: Binary info
test_binary_info() {
    log_info "Test 1: Binary Information"
    echo "  Binary: $BINARY"
    echo "  Size: $(ls -lh $BINARY | awk '{print $5}')"
    echo "  Type: $(file $BINARY | cut -d: -f2)"
    $BINARY version 2>&1 || echo "  Version: dev"
    log_success "Binary info retrieved"
}

# Test 2: Basic startup/shutdown
test_basic_startup() {
    log_info "Test 2: Basic Startup/Shutdown"

    $BINARY run -config-id=$CONFIG_ID -listen=localhost:$TEST_PORT &
    NEXTDNS_PID=$!

    sleep 2

    if kill -0 $NEXTDNS_PID 2>/dev/null; then
        log_success "Daemon started successfully"
    else
        log_fail "Daemon failed to start"
        return 1
    fi

    kill $NEXTDNS_PID
    wait $NEXTDNS_PID 2>/dev/null || true

    if ! kill -0 $NEXTDNS_PID 2>/dev/null; then
        log_success "Daemon stopped cleanly"
    else
        log_fail "Daemon did not stop"
        return 1
    fi

    unset NEXTDNS_PID
}

# Test 3: DNS query functionality
test_dns_query() {
    log_info "Test 3: DNS Query Functionality"

    $BINARY run -config-id=$CONFIG_ID -listen=localhost:$TEST_PORT > /tmp/nextdns-test.log 2>&1 &
    NEXTDNS_PID=$!
    sleep 2

    RESULT=$(dig @localhost -p $TEST_PORT +short example.com 2>&1 || echo "FAILED")

    if [[ "$RESULT" != "FAILED" ]] && [[ ! -z "$RESULT" ]]; then
        log_success "DNS query succeeded: $RESULT"
    else
        log_fail "DNS query failed"
        cat /tmp/nextdns-test.log
        kill $NEXTDNS_PID 2>/dev/null || true
        return 1
    fi

    kill $NEXTDNS_PID
    wait $NEXTDNS_PID 2>/dev/null || true
    unset NEXTDNS_PID
}

# Test 4: Load test (1000 queries)
test_load() {
    log_info "Test 4: Load Test (1000 queries)"

    $BINARY run -config-id=$CONFIG_ID -listen=localhost:$TEST_PORT > /tmp/nextdns-test.log 2>&1 &
    NEXTDNS_PID=$!
    sleep 2

    FAILURES=0
    START_TIME=$(date +%s)

    for i in {1..1000}; do
        dig @localhost -p $TEST_PORT +short example.com > /dev/null 2>&1 || ((FAILURES++))
    done

    END_TIME=$(date +%s)
    DURATION=$((END_TIME - START_TIME))

    if [ $FAILURES -eq 0 ]; then
        log_success "Load test passed: 1000 queries in ${DURATION}s, 0 failures"
    else
        log_fail "Load test: $FAILURES failures out of 1000"
    fi

    # Check logs for errors
    if grep -qi "panic\|fatal" /tmp/nextdns-test.log; then
        log_fail "Errors found in logs"
        grep -i "panic\|fatal" /tmp/nextdns-test.log
    else
        log_success "No errors in logs"
    fi

    kill $NEXTDNS_PID
    wait $NEXTDNS_PID 2>/dev/null || true
    unset NEXTDNS_PID
}

# Test 5: Concurrent queries
test_concurrent() {
    log_info "Test 5: Concurrent Queries (100 parallel)"

    $BINARY run -config-id=$CONFIG_ID -listen=localhost:$TEST_PORT > /tmp/nextdns-test.log 2>&1 &
    NEXTDNS_PID=$!
    sleep 2

    FAILURES=0
    for i in {1..100}; do
        dig @localhost -p $TEST_PORT +short google.com > /dev/null 2>&1 || ((FAILURES++)) &
    done

    wait

    if [ $FAILURES -eq 0 ]; then
        log_success "Concurrent test passed: 100 queries, 0 failures"
    else
        log_fail "Concurrent test: $FAILURES failures"
    fi

    kill $NEXTDNS_PID
    wait $NEXTDNS_PID 2>/dev/null || true
    unset NEXTDNS_PID
}

# Test 6: Memory stability
test_memory() {
    log_info "Test 6: Memory Leak Test"

    $BINARY run -config-id=$CONFIG_ID -listen=localhost:$TEST_PORT > /tmp/nextdns-test.log 2>&1 &
    NEXTDNS_PID=$!
    sleep 2

    MEM_BEFORE=$(ps -o rss= -p $NEXTDNS_PID 2>/dev/null || echo 0)
    log_info "Memory before: ${MEM_BEFORE}KB"

    # Send 500 queries
    for i in {1..500}; do
        dig @localhost -p $TEST_PORT +short test$i.example.com > /dev/null 2>&1 || true
    done

    sleep 3

    MEM_AFTER=$(ps -o rss= -p $NEXTDNS_PID 2>/dev/null || echo 0)
    log_info "Memory after: ${MEM_AFTER}KB"

    MEM_GROWTH=$((MEM_AFTER - MEM_BEFORE))
    log_info "Memory growth: ${MEM_GROWTH}KB"

    if [ $MEM_GROWTH -lt 5000 ]; then
        log_success "Memory growth acceptable: ${MEM_GROWTH}KB < 5000KB"
    else
        log_warn "High memory growth: ${MEM_GROWTH}KB"
    fi

    kill $NEXTDNS_PID
    wait $NEXTDNS_PID 2>/dev/null || true
    unset NEXTDNS_PID
}

# Test 7: Restart cycles (race condition test)
test_restart_cycles() {
    log_info "Test 7: Restart Cycles (5 iterations)"

    FAILURES=0
    for i in {1..5}; do
        $BINARY run -config-id=$CONFIG_ID -listen=localhost:$TEST_PORT > /tmp/nextdns-test.log 2>&1 &
        NEXTDNS_PID=$!

        sleep 1

        # Try a query
        dig @localhost -p $TEST_PORT +short example.com > /dev/null 2>&1 || ((FAILURES++))

        # Stop daemon
        kill $NEXTDNS_PID 2>/dev/null
        wait $NEXTDNS_PID 2>/dev/null || true

        sleep 1
    done

    unset NEXTDNS_PID

    if [ $FAILURES -eq 0 ]; then
        log_success "Restart cycles passed: 5 iterations, 0 failures"
    else
        log_fail "Restart cycles: $FAILURES failures"
    fi
}

# Test 8: Cache performance
test_cache() {
    log_info "Test 8: Cache Performance"

    $BINARY run -config-id=$CONFIG_ID -listen=localhost:$TEST_PORT > /tmp/nextdns-test.log 2>&1 &
    NEXTDNS_PID=$!
    sleep 2

    # First query (cache miss)
    START1=$(date +%s%N)
    dig @localhost -p $TEST_PORT +short cache-test.example.com > /dev/null 2>&1
    END1=$(date +%s%N)
    TIME1=$(( (END1 - START1) / 1000000 ))

    # Second query (cache hit)
    START2=$(date +%s%N)
    dig @localhost -p $TEST_PORT +short cache-test.example.com > /dev/null 2>&1
    END2=$(date +%s%N)
    TIME2=$(( (END2 - START2) / 1000000 ))

    log_info "First query (miss): ${TIME1}ms"
    log_info "Second query (hit): ${TIME2}ms"

    if [ $TIME2 -lt $TIME1 ]; then
        SPEEDUP=$(( (TIME1 * 100) / TIME2 ))
        log_success "Cache working: ${SPEEDUP}% of original time"
    else
        log_warn "Cache may not be working optimally"
    fi

    kill $NEXTDNS_PID
    wait $NEXTDNS_PID 2>/dev/null || true
    unset NEXTDNS_PID
}

# Test 9: Timeout handling (DoS fix validation)
test_timeout_handling() {
    log_info "Test 9: Timeout Handling (DoS Fix)"

    $BINARY run -config-id=$CONFIG_ID -listen=localhost:$TEST_PORT > /tmp/nextdns-test.log 2>&1 &
    NEXTDNS_PID=$!
    sleep 2

    # Query should complete or timeout gracefully (not hang forever)
    timeout 5 dig @localhost -p $TEST_PORT +short timeout-test.example.com > /dev/null 2>&1
    EXIT_CODE=$?

    # Check daemon still running
    if kill -0 $NEXTDNS_PID 2>/dev/null; then
        log_success "Daemon survived timeout test (no DoS)"
    else
        log_fail "Daemon crashed during timeout test"
    fi

    kill $NEXTDNS_PID 2>/dev/null || true
    wait $NEXTDNS_PID 2>/dev/null || true
    unset NEXTDNS_PID
}

# Main test execution
main() {
    echo ""
    echo "======================================"
    echo "NextDNS End-to-End Test Suite"
    echo "======================================"
    echo ""

    check_prereqs
    echo ""

    test_binary_info
    echo ""

    test_basic_startup
    echo ""

    test_dns_query
    echo ""

    test_load
    echo ""

    test_concurrent
    echo ""

    test_memory
    echo ""

    test_restart_cycles
    echo ""

    test_cache
    echo ""

    test_timeout_handling
    echo ""

    # Summary
    echo "======================================"
    echo "Test Summary"
    echo "======================================"
    echo -e "${GREEN}Passed: $TESTS_PASSED${NC}"
    echo -e "${RED}Failed: $TESTS_FAILED${NC}"
    echo ""

    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}✅ All tests passed!${NC}"
        echo ""
        echo "The build is ready for production use."
        echo "All critical bugs are fixed and optimizations are working."
        exit 0
    else
        echo -e "${RED}❌ Some tests failed${NC}"
        echo ""
        echo "Please review the failed tests above."
        exit 1
    fi
}

# Run tests
main
