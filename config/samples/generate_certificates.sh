#!/bin/bash

# --- Configuration ---

# CA Details
CA_KEY="ca.key"
CA_CERT="ca.crt"
CA_SUBJECT="/C=US/ST=California/L=Los Angeles/O=Demo Corp/OU=OpenShift CA/CN=example-ca"

# Server Certificate Details
SERVER_KEY="server.key"
SERVER_CSR="server.csr"
SERVER_CERT="server.crt"
SERVER_SUBJECT="/C=US/ST=California/L=Los Angeles/O=Demo Corp/OU=OpenShift Service/CN=vllm-server.vllm-dist.svc.cluster.local"

# Bundle
CA_BUNDLE="ca-bundle.crt"

# Security Configuration
KEY_SIZE=4096
CA_VALIDITY_DAYS=365
SERVER_VALIDITY_DAYS=90

echo "================================================"
echo "WARNING: This script is for TESTING PURPOSES ONLY"
echo "Do NOT use these certificates in production!"
echo "================================================"
echo

# --- 1. Create the Certificate Authority (CA) ---

echo "Generating CA private key and self-signed certificate..."

# Generate the CA's private key with stronger key size
openssl genrsa -out "${CA_KEY}" ${KEY_SIZE}

# Generate the self-signed CA certificate
openssl req -x509 -new -nodes -key "${CA_KEY}" \
  -sha256 -days ${CA_VALIDITY_DAYS} \
  -subj "${CA_SUBJECT}" \
  -out "${CA_CERT}"

echo "CA created successfully: ${CA_KEY}, ${CA_CERT}"

# --- 2. Create the Server Certificate and Sign with CA ---

echo "Generating server private key and certificate signing request (CSR)..."

# Generate the server's private key
openssl genrsa -out "${SERVER_KEY}" ${KEY_SIZE}

# Generate the server's CSR
openssl req -new -nodes -key "${SERVER_KEY}" \
  -subj "${SERVER_SUBJECT}" \
  -out "${SERVER_CSR}"

echo "Signing server certificate with the CA..."

# Sign the server CSR with the CA key to create the server certificate
openssl x509 -req -in "${SERVER_CSR}" \
  -CA "${CA_CERT}" -CAkey "${CA_KEY}" -CAcreateserial \
  -out "${SERVER_CERT}" -days ${SERVER_VALIDITY_DAYS} -sha256

echo "Server certificate created and signed: ${SERVER_CERT}"

# --- 3. Create the Final Certificate Bundle ---

echo "Creating the final certificate bundle..."

# Concatenate the server certificate and the CA certificate into a single bundle file
cat "${SERVER_CERT}" "${CA_CERT}" > "${CA_BUNDLE}"

echo "   All files created successfully!"
echo "------------------------------------"
echo "  - CA Private Key:           ${CA_KEY} (${KEY_SIZE}-bit)"
echo "  - CA Certificate:           ${CA_CERT} (valid for ${CA_VALIDITY_DAYS} days)"
echo "  - Server Private Key:       ${SERVER_KEY} (${KEY_SIZE}-bit)"
echo "  - Server Certificate:       ${SERVER_CERT} (valid for ${SERVER_VALIDITY_DAYS} days)"
echo "  - Combined Bundle:          ${CA_BUNDLE}"
echo "------------------------------------"
echo
echo "Security Reminders:"
echo "- Keep private keys secure and never commit them to version control"
echo "- Consider using cert-manager for production certificate management"
echo "- Rotate certificates regularly in production environments"

# Clean up the server's CSR as it is no longer needed
rm "${SERVER_CSR}"

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p "${SCRIPT_DIR}/vllm-certs"

cp "${SERVER_CERT}" "${SCRIPT_DIR}/vllm-certs/"
cp "${SERVER_KEY}" "${SCRIPT_DIR}/vllm-certs/"

mkdir -p "${SCRIPT_DIR}/vllm-ca-certs"

cp "${CA_BUNDLE}" "${SCRIPT_DIR}/vllm-ca-certs/"
