#!/bin/bash
# Test script for trusted-proxy verification on OpenShift
# Usage:
#   Test CIDR:  ./test-trusted-proxy.sh cidr
#   Test HMAC:  ./test-trusted-proxy.sh hmac
#   Test both:  ./test-trusted-proxy.sh both

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SVC_NAME="ogxserver-with-userconfig"
SVC_RESOURCE_NAME="ogxserver-with-userconfig-service"
SVC_PORT=8321
LOCAL_PORT=8321
SECRET="spike-test-secret-do-not-use-in-prod"
PF_PID=""

cleanup() {
    if [ -n "$PF_PID" ] && kill -0 "$PF_PID" 2>/dev/null; then
        kill "$PF_PID" 2>/dev/null
        wait "$PF_PID" 2>/dev/null || true
    fi
    rm -f /tmp/proxy-test-response.json
}
trap cleanup EXIT

start_port_forward() {
    # Kill any existing port-forward
    if [ -n "$PF_PID" ] && kill -0 "$PF_PID" 2>/dev/null; then
        kill "$PF_PID" 2>/dev/null
        wait "$PF_PID" 2>/dev/null || true
    fi

    oc port-forward "svc/${SVC_RESOURCE_NAME}" "${LOCAL_PORT}:${SVC_PORT}" &>/dev/null &
    PF_PID=$!

    # Wait until port-forward is ready by polling the public /v1/health endpoint
    echo "   Waiting for port-forward..."
    local retries=0
    while [ $retries -lt 30 ]; do
        if curl -sf -o /dev/null "http://localhost:${LOCAL_PORT}/v1/health" 2>/dev/null; then
            echo "   Port-forward ready."
            return 0
        fi
        sleep 1
        retries=$((retries + 1))
    done
    echo "   ERROR: Port-forward did not become ready after 30 seconds"
    return 1
}

do_curl() {
    # Run curl, capture HTTP code to a separate file to avoid output corruption
    local code_file
    code_file=$(mktemp)
    curl -s -o /tmp/proxy-test-response.json -w '%{http_code}' "$@" > "$code_file" 2>/dev/null || true
    cat "$code_file"
    rm -f "$code_file"
}

compute_hmac() {
    # Canonical signing string: sorted header-name=value joined by newlines
    local user_id="$1"
    shift
    # Remaining args are additional "header=value" pairs
    local -a parts=()
    parts+=("x-user-id=${user_id}")
    for arg in "$@"; do
        parts+=("$arg")
    done
    # Sort
    IFS=$'\n' sorted=($(sort <<<"${parts[*]}")); unset IFS
    local canonical
    canonical=$(printf "%s" "${sorted[0]}")
    for ((i=1; i<${#sorted[@]}; i++)); do
        canonical=$(printf "%s\n%s" "$canonical" "${sorted[$i]}")
    done
    printf '%s' "$canonical" | openssl dgst -sha256 -hmac "$SECRET" -hex 2>/dev/null | awk '{print $NF}'
}

apply_and_wait() {
    local config_file="$1"
    echo "   Applying config..."
    oc apply -f "${config_file}"
    echo "   Waiting for rollout..."
    oc rollout status "deployment/${SVC_NAME}" --timeout=120s 2>/dev/null || true
    # Wait for terminating pods to fully stop so port-forward doesn't latch onto a dying pod
    echo "   Waiting for old pods to terminate..."
    local retries=0
    while [ $retries -lt 30 ]; do
        local terminating
        terminating=$(oc get pods -l "app.kubernetes.io/instance=${SVC_NAME}" --field-selector=status.phase!=Running -o name 2>/dev/null | wc -l)
        if [ "$terminating" -eq 0 ]; then
            break
        fi
        sleep 2
        retries=$((retries + 1))
    done
    sleep 3
}

check_result() {
    local test_name="$1"
    local expected="$2"
    local actual="$3"

    if [ "$actual" = "$expected" ]; then
        echo "   PASS: ${test_name} (HTTP ${actual})"
    else
        echo "   FAIL: ${test_name} — expected HTTP ${expected}, got ${actual}"
        if [ -f /tmp/proxy-test-response.json ]; then
            echo "   Response body:"
            python3 -m json.tool /tmp/proxy-test-response.json 2>/dev/null | sed 's/^/   /' || cat /tmp/proxy-test-response.json | sed 's/^/   /'
        fi
    fi
    echo ""
}

test_cidr() {
    echo "=== CIDR Trusted Proxy Test ==="
    echo ""

    apply_and_wait "${SCRIPT_DIR}/test-trusted-proxy-cidr.yaml"

    # Test from outside the cluster via port-forward
    # Port-forward traffic arrives at the container as 127.0.0.1, which is NOT
    # in 10.128.0.0/14, so this should be rejected.
    echo "1. From outside cluster (port-forward, source IP = 127.0.0.1) — expect 403"
    start_port_forward
    HTTP_CODE=$(do_curl -H "x-user-id: alice" "http://localhost:${LOCAL_PORT}/v1/models")
    check_result "External request rejected by CIDR" "403" "$HTTP_CODE"

    # Verify that the public /v1/health endpoint still works (bypasses auth)
    echo "2. Health endpoint (public, bypasses auth) — expect 200"
    HTTP_CODE=$(do_curl "http://localhost:${LOCAL_PORT}/v1/health")
    check_result "Health endpoint accessible" "200" "$HTTP_CODE"
}

test_hmac() {
    echo "=== HMAC Trusted Proxy Test ==="
    echo ""

    apply_and_wait "${SCRIPT_DIR}/test-trusted-proxy-hmac.yaml"
    start_port_forward

    # Test 1: No signature header — should get 403
    echo "1. Without signature — expect 403"
    HTTP_CODE=$(do_curl -H "x-user-id: alice" "http://localhost:${LOCAL_PORT}/v1/models")
    check_result "Missing signature rejected" "403" "$HTTP_CODE"

    # Test 2: Bad signature — should get 403
    echo "2. With bad signature — expect 403"
    HTTP_CODE=$(do_curl \
        -H "x-user-id: alice" \
        -H "x-ogx-proxy-signature: deadbeef" \
        "http://localhost:${LOCAL_PORT}/v1/models")
    check_result "Bad signature rejected" "403" "$HTTP_CODE"

    # Test 3: Valid signature — should get 200
    # The server signs ALL configured identity headers (principal, tenant, attributes),
    # even when absent from the request (they default to empty string).
    echo "3. With valid signature — expect 200"
    SIG=$(compute_hmac "alice" "x-tenant-id=" "x-auth-attributes=")
    HTTP_CODE=$(do_curl \
        -H "x-user-id: alice" \
        -H "x-ogx-proxy-signature: ${SIG}" \
        "http://localhost:${LOCAL_PORT}/v1/models")
    check_result "Valid signature accepted" "200" "$HTTP_CODE"

    # Test 4: Valid signature for alice, but header says mallory — should get 403
    echo "4. Tampered identity (sig for alice, header says mallory) — expect 403"
    SIG=$(compute_hmac "alice" "x-tenant-id=" "x-auth-attributes=")
    HTTP_CODE=$(do_curl \
        -H "x-user-id: mallory" \
        -H "x-ogx-proxy-signature: ${SIG}" \
        "http://localhost:${LOCAL_PORT}/v1/models")
    check_result "Tampered identity rejected" "403" "$HTTP_CODE"

    # Test 5: Health endpoint still works without any signature
    echo "5. Health endpoint (public, bypasses auth) — expect 200"
    HTTP_CODE=$(do_curl "http://localhost:${LOCAL_PORT}/v1/health")
    check_result "Health endpoint accessible" "200" "$HTTP_CODE"
}

case "${1:-}" in
    cidr)
        test_cidr
        ;;
    hmac)
        test_hmac
        ;;
    both)
        test_cidr
        echo ""
        echo "========================================="
        echo ""
        test_hmac
        ;;
    *)
        echo "Usage: $0 {cidr|hmac|both}"
        exit 1
        ;;
esac

echo "=== All tests complete ==="
